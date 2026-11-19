package api

import (
	"log/slog"
	"net/http"
	"time"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/config"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/ppcdn"
	"ppclassvip/internal/realtime"
	"ppclassvip/internal/settings"
	"ppclassvip/internal/storage"
	"ppclassvip/internal/store"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Dependencies struct {
	Config      config.Config
	DB          *gorm.DB
	Logger      *slog.Logger
	Tokens      *auth.TokenManager
	Users       *store.UserStore
	Classrooms  *store.ClassroomStore
	Hub         *realtime.Hub
	Media       ppcdn.Provider
	Storage     *storage.Local
	Attachments *store.AttachmentStore
	Recordings  *store.RecordingStore
	Audits      *store.AuditStore
	MediaStats  *store.MediaStatStore
	Limiter     *middleware.RateLimiter
	Settings    *settings.Service
	Admin       *store.AdminStore
	Remote      *store.RemoteDesktopStore
	Control     *realtime.ControlHub
}

func NewRouter(deps Dependencies) *gin.Engine {
	if deps.Config.App.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	limiter := deps.Limiter
	if limiter == nil {
		limiter = middleware.NewRateLimiter(time.Minute)
	}
	settingsService := deps.Settings
	if settingsService == nil {
		settingsService = settings.NewStatic(deps.Config.Classroom)
	}

	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.AccessLog(deps.Logger),
		middleware.Recovery(deps.Logger),
		middleware.CORS(deps.Config.HTTP.CORSOrigins),
	)
	r.NoRoute(func(c *gin.Context) {
		Fail(c, http.StatusNotFound, "not found")
	})

	health := NewHealthHandler(deps.DB)
	r.GET("/healthz", health.Health)
	r.GET("/readyz", health.Ready)

	classroomHandler := NewClassroomHandler(ClassroomHandlerDeps{
		Classrooms:  deps.Classrooms,
		Users:       deps.Users,
		Tokens:      deps.Tokens,
		Config:      deps.Config,
		Logger:      deps.Logger,
		Hub:         deps.Hub,
		Media:       deps.Media,
		Storage:     deps.Storage,
		Attachments: deps.Attachments,
		Recordings:  deps.Recordings,
		Audits:      deps.Audits,
		MediaStats:  deps.MediaStats,
		Limiter:     limiter,
		Settings:    settingsService,
		Remote:      deps.Remote,
		Control:     deps.Control,
	})

	adminHandler := NewAdminHandler(AdminHandlerDeps{
		Users:       deps.Users,
		Classrooms:  deps.Classrooms,
		Recordings:  deps.Recordings,
		Attachments: deps.Attachments,
		Audits:      deps.Audits,
		Admin:       deps.Admin,
		Storage:     deps.Storage,
		Settings:    settingsService,
		Hub:         deps.Hub,
		Logger:      deps.Logger,
	})

	v1 := r.Group("/api/v1")
	v1.GET("/ping", func(c *gin.Context) {
		OK(c, gin.H{"pong": true})
	})

	authHandler := NewAuthHandler(deps.Users, deps.Tokens, deps.Logger)
	v1.POST("/auth/login", middleware.RateLimit(limiter, "login", 30), authHandler.Login)
	v1.POST("/agent/pair", middleware.RateLimitFunc(limiter, "agent-pair", func() int { return 30 }), classroomHandler.PairAgent)
	v1.POST("/agent/stop", middleware.RateLimitFunc(limiter, "agent-stop", func() int { return 30 }), classroomHandler.AgentStop)
	v1.POST("/classrooms/join", middleware.RateLimitFunc(limiter, "join", func() int { return settingsService.Classroom().InviteRatePerMinute }), classroomHandler.GuestJoin)
	v1.GET("/classrooms/:id/ws", classroomHandler.ConnectWS)
	v1.GET("/classrooms/:id/remote-desktop/:sid/control", classroomHandler.RemoteControlWS)
	v1.GET("/classrooms/:id/attachments/:aid/download", classroomHandler.DownloadAttachment)
	v1.GET("/classrooms/:id/recordings/:rid/download", classroomHandler.DownloadRecording)

	protected := v1.Group("")
	protected.Use(middleware.AuthRequired(deps.Tokens))
	protected.GET("/auth/me", authHandler.Me)
	protected.POST("/auth/change-password", middleware.RateLimitFunc(limiter, "password", func() int { return 10 }), authHandler.ChangePassword)

	protected.POST("/classrooms", classroomHandler.Create)
	protected.GET("/classrooms", classroomHandler.List)
	protected.POST("/classrooms/:id/invite", middleware.RateLimitFunc(limiter, "invite", func() int { return settingsService.Classroom().InviteRatePerMinute }), classroomHandler.CreateInvite)
	protected.POST("/classrooms/:id/join", classroomHandler.TeacherJoin)
	protected.POST("/classrooms/:id/leave", classroomHandler.Leave)
	protected.GET("/classrooms/:id", classroomHandler.Get)
	protected.POST("/classrooms/:id/messages", middleware.RateLimitFunc(limiter, "chat", func() int { return settingsService.Classroom().MessageRatePerMinute }), classroomHandler.SendMessage)
	protected.GET("/classrooms/:id/messages", classroomHandler.ListMessages)
	protected.POST("/classrooms/:id/end", classroomHandler.End)
	protected.DELETE("/classrooms/:id", classroomHandler.Delete)

	protected.POST("/classrooms/:id/screen/start", classroomHandler.StartScreen)
	protected.POST("/classrooms/:id/screen/stop", classroomHandler.StopScreen)
	protected.POST("/classrooms/:id/media/refresh", classroomHandler.RefreshMedia)
	protected.POST("/classrooms/:id/control", classroomHandler.Control)
	protected.POST("/classrooms/:id/remote-desktop/request", classroomHandler.RequestRemoteDesktop)
	protected.POST("/classrooms/:id/remote-desktop/:sid/consent", classroomHandler.ConsentRemoteDesktop)
	protected.POST("/classrooms/:id/remote-desktop/:sid/decline", classroomHandler.DeclineRemoteDesktop)
	protected.POST("/classrooms/:id/remote-desktop/:sid/stop", classroomHandler.StopRemoteDesktop)
	protected.GET("/classrooms/:id/remote-desktop/:sid", classroomHandler.GetRemoteDesktop)
	protected.GET("/classrooms/:id/audit", classroomHandler.ListAudit)

	protected.POST("/classrooms/:id/attachments", middleware.RateLimitFunc(limiter, "upload", func() int { return settingsService.Classroom().UploadRatePerMinute }), classroomHandler.UploadAttachment)
	protected.GET("/classrooms/:id/attachments/:aid", classroomHandler.GetAttachment)

	protected.POST("/classrooms/:id/recordings/start", classroomHandler.StartRecording)
	protected.GET("/classrooms/:id/recordings", classroomHandler.ListRecordings)
	protected.GET("/classrooms/:id/recordings/:rid", classroomHandler.GetRecording)
	protected.POST("/classrooms/:id/recordings/:rid/consent", classroomHandler.ConsentRecording)
	protected.POST("/classrooms/:id/recordings/:rid/decline", classroomHandler.DeclineRecording)
	protected.PUT("/classrooms/:id/recordings/:rid/chunks/:seq", classroomHandler.UploadRecordingChunk)
	protected.POST("/classrooms/:id/recordings/:rid/finalize", classroomHandler.FinalizeRecording)
	protected.POST("/classrooms/:id/recordings/:rid/abort", classroomHandler.AbortRecording)

	admin := protected.Group("/admin")
	admin.Use(middleware.AdminRequired())
	admin.GET("/dashboard", adminHandler.Dashboard)
	admin.GET("/users", adminHandler.ListUsers)
	admin.POST("/users", adminHandler.CreateUser)
	admin.PUT("/users/:id", adminHandler.UpdateUser)
	admin.GET("/classrooms", adminHandler.ListClassrooms)
	admin.GET("/classrooms/:id", adminHandler.GetClassroom)
	admin.POST("/classrooms/:id/end", adminHandler.EndClassroom)
	admin.GET("/recordings", adminHandler.ListRecordings)
	admin.DELETE("/recordings/:id", adminHandler.DeleteRecording)
	admin.GET("/attachments", adminHandler.ListAttachments)
	admin.DELETE("/attachments/:id", adminHandler.DeleteAttachment)
	admin.GET("/audit", adminHandler.ListAudit)
	admin.GET("/settings", adminHandler.GetSettings)
	admin.PUT("/settings", adminHandler.UpdateSettings)

	return r
}
