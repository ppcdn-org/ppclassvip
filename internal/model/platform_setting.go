package model

import "time"

// PlatformSetting is the single-row runtime configuration managed from the
// admin console. Values are persisted in MySQL and applied without a restart.
type PlatformSetting struct {
	ID                      uint64    `gorm:"primaryKey" json:"id"`
	MaxFileBytes            int64     `gorm:"not null" json:"maxFileBytes"`
	MaxImageBytes           int64     `gorm:"not null" json:"maxImageBytes"`
	RoomQuotaBytes          int64     `gorm:"not null" json:"roomQuotaBytes"`
	MaxChunkBytes           int64     `gorm:"not null" json:"maxChunkBytes"`
	MaxRecordingBytes       int64     `gorm:"not null" json:"maxRecordingBytes"`
	RecordingEnabled        bool      `gorm:"not null;default:true" json:"recordingEnabled"`
	ConsentTTLSeconds       int       `gorm:"not null" json:"consentTtlSeconds"`
	IdleTTLSeconds          int       `gorm:"not null" json:"idleTtlSeconds"`
	AttachmentRetentionDays int       `gorm:"not null" json:"attachmentRetentionDays"`
	RecordingRetentionDays  int       `gorm:"not null" json:"recordingRetentionDays"`
	MessageRatePerMinute    int       `gorm:"not null" json:"messageRatePerMinute"`
	UploadRatePerMinute     int       `gorm:"not null" json:"uploadRatePerMinute"`
	InviteRatePerMinute     int       `gorm:"not null" json:"inviteRatePerMinute"`
	MaxRoomsPerTeacher      int       `gorm:"not null;default:3" json:"maxRoomsPerTeacher"`
	UpdatedBy               uint64    `gorm:"not null;default:0" json:"updatedBy,omitempty"`
	UpdatedAt               time.Time `json:"updatedAt"`
}

func (PlatformSetting) TableName() string {
	return "platform_settings"
}
