package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	"github.com/nlopes/slack"
	"golang.org/x/sync/syncmap"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Config struct {
	Port        uint16 `default:"8080"`
	RequestPath string `default:"/ask" split_words:"true"`

	ApiToken          string `required:"true" split_words:"true"`
	VerificationToken string `required:"true" split_words:"true"`

	UserName  string `split_words:"true"`
	IconEmoji string `split_words:"true"`
}

var (
	config      Config
	resultChMap *syncmap.Map = &syncmap.Map{}
)

func main() {
	if err := envconfig.Process("", &config); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	http.HandleFunc(config.RequestPath, postMessageHandler)
	http.HandleFunc("/interactive_action_callback", interactiveActionHandler)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", config.Port), nil); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}
}

func interactiveActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("[ERROR] Invalid method: %s", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("[ERROR] Failed to read request body: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	jsonStr, err := url.QueryUnescape(string(buf)[8:])
	if err != nil {
		log.Printf("[ERROR] Failed to unespace request body: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var callbackPayload slack.AttachmentActionCallback
	if err := json.Unmarshal([]byte(jsonStr), &callbackPayload); err != nil {
		log.Printf("[ERROR] Failed to decode json message from slack: %s", jsonStr)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if callbackPayload.Token != config.VerificationToken {
		log.Printf("[ERROR] Invalid token: %s", callbackPayload.Token)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	ch, ok := resultChMap.Load(callbackPayload.CallbackID)
	if !ok {
		log.Printf("[ERROR] Invalid callback_id: %s", callbackPayload.CallbackID)
		w.WriteHeader(http.StatusNotFound)
	}

	msg := callbackPayload.OriginalMessage
	switch callbackPayload.Actions[0].Name {
	case "approve":
		ch.(chan bool) <- true
		msg.Attachments[0].Text = fmt.Sprintf("Approved by @%s", callbackPayload.User.Name)
		msg.Attachments[0].Color = "good"
	case "cancel":
		ch.(chan bool) <- false
		msg.Attachments[0].Text = fmt.Sprintf("Canceled by @%s", callbackPayload.User.Name)
		msg.Attachments[0].Color = "danger"
	default:
		log.Printf("[ERROR] Unknown action: %s", callbackPayload.Actions[0].Name)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	msg.Attachments[0].Actions = []slack.AttachmentAction{}
	w.Header().Add("Content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(&msg)
}

func postMessageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Connection", "Close")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	channel := r.Form.Get("ch")
	message := r.Form.Get("msg")
	timeout, err := parseTimeout(r.Form.Get("timeout"))
	if channel == "" || message == "" || err != nil {
		w.Header().Add("Content-type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"result":"invalid request"}`))
		return
	}

	callbackId := uuid.New().String()
	ch := make(chan bool)
	resultChMap.Store(callbackId, ch)
	defer resultChMap.Delete(callbackId)

	log.Printf("[INFO] (%s) New request from %s", callbackId, r.RemoteAddr)

	attachment := slack.Attachment{
		Pretext:    message,
		Text:       "Waiting for someone's approval...",
		Color:      "warning",
		CallbackID: callbackId,
		Actions: []slack.AttachmentAction{
			{
				Name:  "approve",
				Text:  "Approve",
				Type:  "button",
				Style: "primary",
			},
			{
				Name:  "cancel",
				Text:  "Cancel",
				Type:  "button",
				Style: "danger",
			},
		},
	}
	params := slack.PostMessageParameters{}
	params.Username = config.UserName
	params.IconEmoji = config.IconEmoji
	params.Attachments = []slack.Attachment{attachment}

	client := slack.New(config.ApiToken)
	channelID, timestamp, err := client.PostMessage(channel, "", params)
	if err != nil {
		log.Fatalf("[ERROR] (%s) %v", callbackId, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	log.Printf("[INFO] (%s) Message successfully sent to channel %s at %s", callbackId, channelID, timestamp)

	select {
	case approved := <-ch:
		if approved {
			log.Printf("[INFO] (%s) Approved", callbackId)
			w.Header().Add("Content-type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result":"approved"}`))
		} else {
			log.Printf("[INFO] (%s) Canceled", callbackId)
			w.Header().Add("Content-type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"result":"canceled"}`))
		}
	case <-time.After(time.Second * time.Duration(timeout)):
		log.Printf("[INFO] (%s) Expired", callbackId)
		attachment.Text = "This approval request was expired."
		attachment.Actions = nil
		attachment.Color = "danger"
		_, _, _, err = client.SendMessage(channelID, slack.MsgOptionUpdate(timestamp), slack.MsgOptionAttachments(attachment))
		if err != nil {
			log.Fatalf("[ERROR] (%s) %v", callbackId, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Add("Content-type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"result":"expired"}`))
	}
}

const (
	DEFAULT_TIMEOUT = 60
	MAX_TIMEOUT     = 600
)

func parseTimeout(str string) (int, error) {
	if str == "" {
		return DEFAULT_TIMEOUT, nil
	}
	t, err := strconv.Atoi(str)
	if err != nil {
		return 0, err
	}
	if t > MAX_TIMEOUT || t < 0 {
		return 0, errors.New("invalid timeout")
	}

	return t, nil
}
