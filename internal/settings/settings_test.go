package settings

import (
	"context"
	"errors"
	"testing"

	"ppclassvip/internal/config"
	"ppclassvip/internal/model"
)

func TestUpdateAppliesRuntimeValues(t *testing.T) {
	cfg := config.Default().Classroom
	service := NewStatic(cfg)
	input := service.Values()
	input.RecordingEnabled = false
	input.MaxFileBytes = 12 << 20
	input.MaxImageBytes = 4 << 20
	input.RoomQuotaBytes = 100 << 20
	input.MaxChunkBytes = 2 << 20
	input.MaxRecordingBytes = 50 << 20
	input.ConsentTTLSeconds = 30
	input.IdleTTLSeconds = 120
	input.AttachmentRetentionDays = 30
	input.RecordingRetentionDays = 10
	input.MessageRatePerMinute = 50
	input.UploadRatePerMinute = 10
	input.InviteRatePerMinute = 10
	input.MaxRoomsPerTeacher = 2

	updated, err := service.Update(context.Background(), input, 7)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RecordingEnabled || updated.MaxFileBytes != 12<<20 {
		t.Fatalf("updated = %#v", updated)
	}
	if service.RecordingEnabled() || service.MaxRoomsPerTeacher() != 2 {
		t.Fatalf("runtime getters were not updated")
	}
	if got := service.Classroom().MaxFileBytes; got != 12<<20 {
		t.Fatalf("Classroom().MaxFileBytes = %d", got)
	}
}

func TestUpdateRejectsInvalidValues(t *testing.T) {
	service := NewStatic(config.Default().Classroom)
	input := service.Values()
	input.ConsentTTLSeconds = 1
	if _, err := service.Update(context.Background(), input, 1); !errors.Is(err, ErrInvalidSetting) {
		t.Fatalf("err = %v", err)
	}
	if service.Values().ConsentTTLSeconds == 1 {
		t.Fatal("invalid value was applied")
	}
}

var _ = model.PlatformSetting{}
