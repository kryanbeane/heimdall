package slack

import (
	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	slackclient "github.com/slack-go/slack"
	corev1 "k8s.io/api/core/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Notification struct {
	Name string
}

// SendEvent SendMessage sends a message using all current senders
func SendEvent(u u.Unstructured, url string, priority string, secret corev1.Secret, configMap corev1.ConfigMap) error {
	token := secret.Data["slack-token"]
	channel := configMap.Data["slack-channel"]

	// Traffic light system for priority
	var messageColour string
	var emoji string
	if priority == "high" {
		emoji = ":large_red_circle: :large_red_circle: :large_red_circle:"
		messageColour = "#ff1100"
	} else if priority == "medium" {
		emoji = ":large_orange_circle: :large_orange_circle: :large_orange_circle:"
		messageColour = "#ff6600"
	} else {
		emoji = ":large_yellow_circle: :large_yellow_circle: :large_yellow_circle:"
		messageColour = "#ffcc00"
	}

	//var messageColour string
	//if priority == "high" {
	//	messageColour = ""
	//} else if priority == "medium" {
	//	emoji = ":large_orange_circle: :large_orange_circle: :large_orange_circle:"
	//} else {
	//	emoji = ":large_yellow_circle: :large_yellow_circle: :large_yellow_circle:"
	//}

	api := slackclient.New(string(token))
	attachment := slackclient.Attachment{
		Fields: []slackclient.AttachmentField{
			{
				Title: emoji + "Resource Change Notification" + emoji,
				Value: u.GetName() + "/" + u.GetNamespace(),
			},
			{
				Title: "Example Reason",
				Value: "Resource was changed by Operator X",
				Short: true,
			},
		},
		Color: messageColour,
		Actions: []slackclient.AttachmentAction{
			{
				Type: "button",
				Text: "View Affected Resource",
				URL:  url,
			},
		},
		Footer: "Notifications provided by Heimdall",
	}

	// test slack auth
	if _, err := api.AuthTest(); err != nil {
		logrus.Errorf("Slack auth test failed: %v", err)
		return err
	}

	// Send message to Slack
	if _, _, err := api.PostMessage(
		channel,
		slack.MsgOptionAttachments(attachment),
		slackclient.MsgOptionAsUser(true),
	); err != nil {
		return err
	} else {
		return nil
	}
}
