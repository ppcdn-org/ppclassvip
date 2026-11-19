package model

import "time"

const (
	RemoteDesktopModeViewOnly   = "view_only"
	RemoteDesktopModeControl    = "control"
	RemoteDesktopStatusPending  = "pending_consent"
	RemoteDesktopStatusActive   = "active"
	RemoteDesktopStatusStopped  = "stopped"
	RemoteDesktopStatusDeclined = "declined"
	RemoteDesktopStatusExpired  = "expired"

	RemoteDesktopRoleController = "controller"
	RemoteDesktopRoleAgent      = "agent"
)

type RemoteDesktopSession struct {
	ID                   uint64     `gorm:"primaryKey" json:"id"`
	SessionID            string     `gorm:"size:40;uniqueIndex;not null" json:"sessionId"`
	RoomID               uint64     `gorm:"not null;index" json:"roomId"`
	TeacherParticipantID uint64     `gorm:"not null;index" json:"teacherParticipantId"`
	StudentParticipantID uint64     `gorm:"not null;index" json:"studentParticipantId"`
	Mode                 string     `gorm:"size:16;not null;default:view_only" json:"mode"`
	Status               string     `gorm:"size:32;not null;default:pending_consent;index" json:"status"`
	ConsentAt            *time.Time `json:"consentAt,omitempty"`
	StartedAt            *time.Time `json:"startedAt,omitempty"`
	EndedAt              *time.Time `json:"endedAt,omitempty"`
	ExpiresAt            *time.Time `json:"expiresAt,omitempty"`
	EndReason            string     `gorm:"size:32;not null;default:''" json:"endReason,omitempty"`
	AgentVersion         string     `gorm:"size:32;not null;default:''" json:"agentVersion,omitempty"`
	Platform             string     `gorm:"size:32;not null;default:''" json:"platform,omitempty"`
	AgentCodeHash        string     `gorm:"size:64;not null;default:'';index" json:"-"`
	AgentCodeExpiresAt   *time.Time `json:"agentCodeExpiresAt,omitempty"`
	LastControlSeq       uint64     `gorm:"not null;default:0" json:"lastControlSeq"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

func (RemoteDesktopSession) TableName() string {
	return "remote_desktop_sessions"
}
