package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	ClassroomStateCreated = "created"
	ClassroomStateWaiting = "waiting"
	ClassroomStateActive  = "active"
	ClassroomStateEnded   = "ended"
	ClassroomStateFailed  = "failed"
	ClassroomStateExpired = "expired"

	ParticipantRoleTeacher = "teacher"
	ParticipantRoleStudent = "student"

	MessageTypeText   = "text"
	MessageTypeImage  = "image"
	MessageTypeFile   = "file"
	MessageTypeSystem = "system"

	AttachmentKindImage = "image"
	AttachmentKindFile  = "file"

	AttachmentStatusStored      = "stored"
	AttachmentStatusPurged      = "purged"
	AttachmentStatusQuarantined = "quarantined"

	RecordingStateOff            = "off"
	RecordingStatePendingConsent = "pending_consent"
	RecordingStateRecording      = "recording"
	RecordingStateFinalizing     = "finalizing"
	RecordingStateCompleted      = "completed"
	RecordingStateDeclined       = "declined"
	RecordingStateFailed         = "failed"
	RecordingStateInterrupted    = "interrupted"
	RecordingStatePurged         = "purged"

	EndReasonTeacherEnd   = "teacher_end"
	EndReasonStudentLeft  = "student_left"
	EndReasonTimeout      = "timeout"
	EndReasonMediaError   = "media_error"
	EndReasonNetworkError = "network_error"
)

type Classroom struct {
	ID                       uint64     `gorm:"primaryKey" json:"id"`
	RoomCode                 string     `gorm:"size:32;uniqueIndex;not null" json:"roomCode"`
	Title                    string     `gorm:"size:120;not null;default:''" json:"title"`
	TeacherUserID            uint64     `gorm:"not null;index" json:"teacherUserId"`
	StudentUserID            *uint64    `gorm:"index" json:"studentUserId,omitempty"`
	State                    string     `gorm:"size:32;not null;default:created;index" json:"state"`
	AnchorNodeID             uint32     `gorm:"not null;default:0" json:"anchorNodeId"`
	RecordingState           string     `gorm:"size:32;not null;default:off" json:"recordingState"`
	ScreenOwnerParticipantID *uint64    `gorm:"index" json:"screenOwnerParticipantId,omitempty"`
	ScreenLeg                string     `gorm:"size:32;not null;default:''" json:"screenLeg,omitempty"`
	ChatEnabled              bool       `gorm:"not null;default:true" json:"chatEnabled"`
	StudentScreenAllowed     bool       `gorm:"not null;default:false" json:"studentScreenAllowed"`
	EndReason                string     `gorm:"size:32;not null;default:''" json:"endReason,omitempty"`
	StartedAt                *time.Time `json:"startedAt,omitempty"`
	EndedAt                  *time.Time `json:"endedAt,omitempty"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

func (Classroom) TableName() string {
	return "classroom_rooms"
}

type ClassroomParticipant struct {
	ID          uint64     `gorm:"primaryKey" json:"id"`
	RoomID      uint64     `gorm:"not null;index:idx_participant_room;uniqueIndex:uk_participant_room_user,priority:1" json:"roomId"`
	UserID      *uint64    `gorm:"uniqueIndex:uk_participant_room_user,priority:2" json:"userId,omitempty"`
	DisplayName string     `gorm:"size:64;not null" json:"displayName"`
	Role        string     `gorm:"size:32;not null;index" json:"role"`
	Guest       bool       `gorm:"not null;default:false" json:"guest"`
	Muted       bool       `gorm:"not null;default:false" json:"muted"`
	JoinedAt    time.Time  `gorm:"not null" json:"joinedAt"`
	LastSeenAt  time.Time  `gorm:"not null" json:"lastSeenAt"`
	LeftAt      *time.Time `json:"leftAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func (ClassroomParticipant) TableName() string {
	return "classroom_participants"
}

type ClassroomInvite struct {
	ID        uint64     `gorm:"primaryKey" json:"id"`
	RoomID    uint64     `gorm:"not null;index" json:"roomId"`
	TokenHash string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Role      string     `gorm:"size:32;not null;default:student" json:"role"`
	ExpiresAt time.Time  `gorm:"not null;index" json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	CreatedBy uint64     `gorm:"not null" json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
}

func (ClassroomInvite) TableName() string {
	return "classroom_invites"
}

type ClassroomMessage struct {
	ID                  uint64               `gorm:"primaryKey" json:"id"`
	RoomID              uint64               `gorm:"not null;index:idx_message_room_id,priority:1;uniqueIndex:uk_message_client,priority:1" json:"roomId"`
	SenderParticipantID uint64               `gorm:"not null;index;uniqueIndex:uk_message_client,priority:2" json:"senderParticipantId"`
	SenderRole          string               `gorm:"size:32;not null" json:"senderRole"`
	Type                string               `gorm:"size:32;not null;default:text" json:"type"`
	Body                string               `gorm:"size:2000;not null;default:''" json:"body"`
	AttachmentID        *uint64              `gorm:"index" json:"attachmentId,omitempty"`
	ClientMsgID         string               `gorm:"size:64;not null;uniqueIndex:uk_message_client,priority:3" json:"clientMsgId"`
	CreatedAt           time.Time            `json:"createdAt"`
	DeletedAt           gorm.DeletedAt       `gorm:"index" json:"-"`
	Attachment          *ClassroomAttachment `gorm:"-" json:"attachment,omitempty"`
}

func (ClassroomMessage) TableName() string {
	return "classroom_messages"
}

type ClassroomAttachment struct {
	ID                    uint64     `gorm:"primaryKey" json:"id"`
	RoomID                uint64     `gorm:"not null;index" json:"roomId"`
	UploaderParticipantID uint64     `gorm:"not null;index" json:"uploaderParticipantId"`
	Kind                  string     `gorm:"size:16;not null;default:file" json:"kind"`
	FileKey               string     `gorm:"size:128;uniqueIndex;not null" json:"-"`
	OriginalName          string     `gorm:"size:255;not null;default:''" json:"originalName"`
	ContentType           string     `gorm:"size:128;not null;default:''" json:"contentType"`
	Size                  int64      `gorm:"not null;default:0" json:"size"`
	SHA256                string     `gorm:"size:64;not null;default:''" json:"sha256"`
	Status                string     `gorm:"size:16;not null;default:stored;index" json:"status"`
	CreatedAt             time.Time  `json:"createdAt"`
	PurgedAt              *time.Time `json:"purgedAt,omitempty"`
}

func (ClassroomAttachment) TableName() string {
	return "classroom_attachments"
}

type ClassroomRecording struct {
	ID                    uint64     `gorm:"primaryKey" json:"id"`
	RoomID                uint64     `gorm:"not null;index" json:"roomId"`
	RecordingID           string     `gorm:"size:40;uniqueIndex;not null" json:"recordingId"`
	UploaderParticipantID uint64     `gorm:"not null;index" json:"uploaderParticipantId"`
	Format                string     `gorm:"size:16;not null;default:webm" json:"format"`
	ContentType           string     `gorm:"size:64;not null;default:video/webm" json:"contentType"`
	FileKey               string     `gorm:"size:128;not null;default:''" json:"-"`
	DurationSeconds       int64      `gorm:"not null;default:0" json:"durationSeconds"`
	SizeBytes             int64      `gorm:"not null;default:0" json:"sizeBytes"`
	LastChunkSeq          int64      `gorm:"not null;default:-1" json:"lastChunkSeq"`
	IncludesStudentAudio  bool       `gorm:"not null;default:true" json:"includesStudentAudio"`
	ConsentAt             *time.Time `json:"consentAt,omitempty"`
	ConsentBy             *uint64    `json:"consentBy,omitempty"`
	StartedAt             time.Time  `gorm:"not null" json:"startedAt"`
	EndedAt               *time.Time `json:"endedAt,omitempty"`
	Status                string     `gorm:"size:32;not null;default:pending_consent;index" json:"status"`
	Error                 string     `gorm:"size:255;not null;default:''" json:"error,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

func (ClassroomRecording) TableName() string {
	return "classroom_recordings"
}

type ClassroomAuditLog struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	RoomID        uint64    `gorm:"not null;index" json:"roomId"`
	ParticipantID uint64    `gorm:"not null;default:0;index" json:"participantId,omitempty"`
	UserID        uint64    `gorm:"not null;default:0;index" json:"userId,omitempty"`
	Action        string    `gorm:"size:64;not null;index" json:"action"`
	Detail        string    `gorm:"size:512;not null;default:''" json:"detail,omitempty"`
	ClientIP      string    `gorm:"size:64;not null;default:''" json:"clientIp,omitempty"`
	CreatedAt     time.Time `gorm:"index" json:"createdAt"`
}

func (ClassroomAuditLog) TableName() string {
	return "classroom_audit_logs"
}

type ClassroomMediaStat struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	RoomID        uint64    `gorm:"not null;index" json:"roomId"`
	ParticipantID uint64    `gorm:"not null;index" json:"participantId"`
	Leg           string    `gorm:"size:32;not null;default:''" json:"leg"`
	RTTMs         int       `gorm:"not null;default:0" json:"rttMs"`
	LossPct       float64   `gorm:"not null;default:0" json:"lossPct"`
	JitterMs      int       `gorm:"not null;default:0" json:"jitterMs"`
	BitrateBps    int64     `gorm:"not null;default:0" json:"bitrateBps"`
	CreatedAt     time.Time `gorm:"index" json:"createdAt"`
}

func (ClassroomMediaStat) TableName() string {
	return "classroom_media_stats"
}

func AllModels() []any {
	return []any{
		&User{},
		&Classroom{},
		&ClassroomParticipant{},
		&ClassroomInvite{},
		&ClassroomMessage{},
		&ClassroomAttachment{},
		&ClassroomRecording{},
		&ClassroomAuditLog{},
		&ClassroomMediaStat{},
		&PlatformSetting{},
		&RemoteDesktopSession{},
	}
}
