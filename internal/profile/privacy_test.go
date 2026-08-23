package profile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPhotoMetadataIsNeverSerializedToJSON(t *testing.T) {
	now := time.Now().UTC()
	value, err := json.Marshal(Profile{
		UID:             "user-a",
		PhotoMediaID:    "media-secreto",
		PhotoObjectPath: "avatars/user-a/media-secreto.jpg",
		PhotoUpdatedAt:  &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(value)
	for _, forbidden := range []string{"photoMediaId", "photoObjectPath", "photoUpdatedAt", "media-secreto", "avatars/user-a"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("JSON expôs %q: %s", forbidden, serialized)
		}
	}
}
