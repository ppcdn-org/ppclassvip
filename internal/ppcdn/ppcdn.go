package ppcdn

import (
	"context"
	"net/http"
	"strconv"
)

const (
	LegTeacherCamera = "teacher-cam"
	LegStudentCamera = "student-cam"
	LegTeacherScreen = "teacher-screen"
	LegStudentScreen = "student-screen"

	MediaKindPublish   = "publish"
	MediaKindSubscribe = "subscribe"
)

type Credential struct {
	URL         string `json:"url"`
	BearerToken string `json:"bearerToken,omitempty"`
	Codec       string `json:"codec,omitempty"`
	StreamPath  string `json:"streamPath"`
	ExpiresAt   string `json:"expiresAt"`
}

type Provider interface {
	Publish(ctx context.Context, streamName string) (Credential, error)
	Subscribe(ctx context.Context, streamName string) (Credential, error)
}

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return http.StatusText(e.Status)
}

func StreamName(roomID uint64, leg string) string {
	base := "cls" + strconv.FormatUint(roomID, 10)
	switch leg {
	case LegTeacherScreen:
		return base + "-ts"
	case LegStudentScreen:
		return base + "-ss"
	case LegStudentCamera:
		return base + "-s"
	default:
		return base + "-t"
	}
}

func CameraLeg(role string) string {
	if role == "student" {
		return LegStudentCamera
	}
	return LegTeacherCamera
}

func ScreenLeg(role string) string {
	if role == "student" {
		return LegStudentScreen
	}
	return LegTeacherScreen
}

func PeerCameraLeg(role string) string {
	if role == "student" {
		return LegTeacherCamera
	}
	return LegStudentCamera
}

func PeerScreenLeg(role string) string {
	if role == "student" {
		return LegTeacherScreen
	}
	return LegStudentScreen
}
