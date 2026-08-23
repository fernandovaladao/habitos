package reminder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type WebPushSender struct{ publicKey, privateKey, subscriber string }

func NewWebPushSender(publicKey, privateKey, subscriber string) *WebPushSender {
	return &WebPushSender{publicKey: publicKey, privateKey: privateKey, subscriber: subscriber}
}

func (s *WebPushSender) Send(ctx context.Context, subscription Subscription, message PushMessage) (bool, error) {
	payload, _ := json.Marshal(message)
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{Endpoint: subscription.Endpoint, Keys: webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth}}, &webpush.Options{Subscriber: s.subscriber, VAPIDPublicKey: s.publicKey, VAPIDPrivateKey: s.privateKey, TTL: 1800})
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 4096)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return false, nil
	}
	permanent := response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone
	return permanent, ErrInvalidSubscription
}
