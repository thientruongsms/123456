package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	_ "github.com/heroku/x/hmetrics/onload"
	"github.com/nlopes/slack"
)

type MessageIMEventJSON struct {
	EventType string `json:"type"`
	Channel   string `json:"channel"`
	User      string `json:"user"`
	Text      string `json:"text"`
	TS        string `json:"ts"`
}

type SlackPayloadJSON struct {
	Token    string             `json:"token"`
	TeamID   string             `json:"team_id"`
	APIAppID string             `json:"api_app_id"`
	EventID  string             `json:"event_id"`
	Event    MessageIMEventJSON `json:"event"`
}

func validate(payloadjson *SlackPayloadJSON) bool {
	credentials := [4]string{
		os.Getenv("SLACK_PAYLOAD_TOKEN"),
		os.Getenv("TEAM_ID"),
		os.Getenv("APP_ID"),
		os.Getenv("MY_DM_CHANNEL"),
	}

	received_credentials := [4]string{
		payloadjson.Token,
		payloadjson.TeamID,
		payloadjson.APIAppID,
		payloadjson.Event.Channel,
	}

	if credentials != received_credentials {
		return false
	}

	return strings.Contains(payloadjson.Event.Text, "<@USLACKBOT> uploaded a file:")
}

func create_empty_file(fname string) {
	d1 := []byte("")
	ioutil.WriteFile(fname, d1, 0644)
}

func main() {
	port := os.Getenv("PORT")

	if port == "" {
		log.Fatal("$PORT must be set")
	}

	router := gin.New()
	router.Use(gin.Logger())

	router.POST("/", func(c *gin.Context) {

		var payloadjson SlackPayloadJSON

		bind_err := c.Bind(&payloadjson)
		valid_req := validate(&payloadjson)

		if bind_err == nil && valid_req {
			// We need the file id in the message which is present inside the URL of the file
			// starting with F.
			re := regexp.MustCompile(`/F\w*/`)
			file_id := re.FindString(payloadjson.Event.Text)
			file_id = file_id[1 : len(file_id)-1]
			log.Println("Got the file id", file_id)

			// Slack API sucks. They send two payloads for message.im event and we
			// only have to respond to one. So, once we send the email to channel, we
			// create an empty file with that name, and thus do not respond to a payload
			// if a file with name of the particular file id exists.
			curr_files, _ := filepath.Glob("*")
			filename := file_id + ".email_dmp"
			for _, f := range curr_files {
				if f == filename {
					log.Println("Email already sent to the channel.")
					return
				}
			}
			defer create_empty_file(filename)

			// Prepare to get file content
			files_info_url := "https://slack.com/api/files.info"
			v := url.Values{}
			v.Set("file", file_id)
			v.Set("token", os.Getenv("SLACK_WORKSPACE_TOKEN_FOR_APP"))
			s := v.Encode()
			req, err := http.NewRequest("POST", files_info_url, strings.NewReader(s))
			req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("http.Do() error: %v\n", err)
				return
			}

			defer resp.Body.Close()

			body, err := ioutil.ReadAll(resp.Body)

			log.Println("BODY IS THIS : ", string(body))

			var data map[string]interface{}
			if err := json.Unmarshal(body, &data); err != nil {
				panic(err)
			}
			file := data["file"].(map[string]interface{})

			message_to_send := "New email\n Subject: `%s` \n\n ```%s```"
			message_to_send = fmt.Sprintf(message_to_send, file["subject"], file["plain_text"])
			log.Println(message_to_send)

			// Send the message
			api := slack.New(os.Getenv("SLACK_BOT_TOKEN"))
			params := slack.NewPostMessageParameters()
			params.Text = message_to_send
			params.Username = "bhattu"
			params.AsUser = true
			params.Parse = "full"

			api.PostMessage(os.Getenv("CHANNEL_ID"), message_to_send, params)

			c.JSON(200, gin.H{"success": true})
		} else {
			c.JSON(404, gin.H{"success": false})
		}
	})

	router.Run(":" + port)
}
