package slack

import (
	"github.com/slack-go/slack"
	slackclient "github.com/slack-go/slack"
	corev1 "k8s.io/api/core/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Notification struct {
	Name string
}

// SendEvent SendMessage sends a message using all current senders
func SendEvent(u u.Unstructured, secret corev1.Secret, configMap corev1.ConfigMap) error {
	token := secret.Data["slack-token"]
	channel := configMap.Data["slack-channel"]

	api := slackclient.New(string(token))
	attachment := slackclient.Attachment{
		Fields: []slackclient.AttachmentField{
			{
				Title: "Object Name: " + u.GetName(),
			},
			{
				Title: "Oh no! Please monitor your resource!",
			},
		},
	}

	// Send message to Slack
	_, _, err := api.PostMessage(
		channel,
		slack.MsgOptionAttachments(attachment),
		slackclient.MsgOptionAsUser(true),
	)
	if err != nil {
		return err
	} else {
		return nil
	}
}