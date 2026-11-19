package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultDevJWTSecret = "dev-only-insecure-secret-change-me"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || strings.TrimSpace(value.Value) == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

type Config struct {
	App       AppConfig       `yaml:"app"`
	HTTP      HTTPConfig      `yaml:"http"`
	Log       LogConfig       `yaml:"log"`
	MySQL     MySQLConfig     `yaml:"mysql"`
	Auth      AuthConfig      `yaml:"auth"`
	Classroom ClassroomConfig `yaml:"classroom"`
	Storage   StorageConfig   `yaml:"storage"`
	PPCDN     PPCDNConfig     `yaml:"ppcdn"`
}

type AppConfig struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"`
}

type HTTPConfig struct {
	Addr            string   `yaml:"addr"`
	ReadTimeout     Duration `yaml:"readTimeout"`
	WriteTimeout    Duration `yaml:"writeTimeout"`
	ShutdownTimeout Duration `yaml:"shutdownTimeout"`
	CORSOrigins     []string `yaml:"corsOrigins"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type MySQLConfig struct {
	DSN             string   `yaml:"dsn"`
	MaxOpenConns    int      `yaml:"maxOpenConns"`
	MaxIdleConns    int      `yaml:"maxIdleConns"`
	ConnMaxLifetime Duration `yaml:"connMaxLifetime"`
	ConnMaxIdleTime Duration `yaml:"connMaxIdleTime"`
	AutoMigrate     bool     `yaml:"autoMigrate"`
}

type AuthConfig struct {
	JWTSecret string   `yaml:"jwtSecret"`
	Issuer    string   `yaml:"issuer"`
	AccessTTL Duration `yaml:"accessTTL"`
}

type ClassroomConfig struct {
	InviteTTL            Duration `yaml:"inviteTTL"`
	RoomTokenTTL         Duration `yaml:"roomTokenTTL"`
	ConsentTTL           Duration `yaml:"consentTTL"`
	IdleTTL              Duration `yaml:"idleTTL"`
	MaxFileBytes         int64    `yaml:"maxFileBytes"`
	MaxImageBytes        int64    `yaml:"maxImageBytes"`
	RoomQuotaBytes       int64    `yaml:"roomQuotaBytes"`
	AttachmentRetention  Duration `yaml:"attachmentRetention"`
	MessageRatePerMinute int      `yaml:"messageRatePerMinute"`
	UploadRatePerMinute  int      `yaml:"uploadRatePerMinute"`
	InviteRatePerMinute  int      `yaml:"inviteRatePerMinute"`
}

type StorageConfig struct {
	DataDir       string `yaml:"dataDir"`
	AttachmentDir string `yaml:"attachmentDir"`
}

type PPCDNConfig struct {
	Enabled   bool     `yaml:"enabled"`
	BaseURL   string   `yaml:"baseUrl"`
	AppID     string   `yaml:"appId"`
	AppSecret string   `yaml:"appSecret"`
	KeyID     string   `yaml:"keyId"`
	DeviceID  string   `yaml:"deviceId"`
	Region    string   `yaml:"region"`
	Timeout   Duration `yaml:"timeout"`
	MediaTTL  Duration `yaml:"mediaTTL"`
}

func Default() Config {
	return Config{
		App: AppConfig{
			Name: "PPClassVip",
			Env:  "dev",
		},
		HTTP: HTTPConfig{
			Addr:            ":18080",
			ReadTimeout:     Duration(15 * time.Second),
			WriteTimeout:    Duration(15 * time.Second),
			ShutdownTimeout: Duration(10 * time.Second),
			CORSOrigins:     []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		MySQL: MySQLConfig{
			DSN:             "root:ppclassvip@tcp(127.0.0.1:3306)/ppclassvip?charset=utf8mb4&parseTime=True&loc=Local",
			MaxOpenConns:    20,
			MaxIdleConns:    10,
			ConnMaxLifetime: Duration(30 * time.Minute),
			ConnMaxIdleTime: Duration(60 * time.Second),
			AutoMigrate:     true,
		},
		Auth: AuthConfig{
			Issuer:    "ppclassvip",
			AccessTTL: Duration(24 * time.Hour),
		},
		Classroom: ClassroomConfig{
			InviteTTL:            Duration(24 * time.Hour),
			RoomTokenTTL:         Duration(12 * time.Hour),
			ConsentTTL:           Duration(60 * time.Second),
			IdleTTL:              Duration(4 * time.Hour),
			MaxFileBytes:         50 << 20,
			MaxImageBytes:        10 << 20,
			RoomQuotaBytes:       500 << 20,
			AttachmentRetention:  Duration(365 * 24 * time.Hour),
			MessageRatePerMinute: 120,
			UploadRatePerMinute:  30,
			InviteRatePerMinute:  30,
		},
		Storage: StorageConfig{
			DataDir: "./data",
		},
		PPCDN: PPCDNConfig{
			KeyID:    "default",
			Timeout:  Duration(10 * time.Second),
			MediaTTL: Duration(10 * time.Minute),
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return cfg, fmt.Errorf("read config %s: %w", path, err)
			}
		} else if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnv(&cfg)

	if cfg.Auth.JWTSecret == "" && !strings.EqualFold(cfg.App.Env, "prod") {
		cfg.Auth.JWTSecret = DefaultDevJWTSecret
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	setString(&cfg.App.Name, "PPCLASS_APP_NAME")
	setString(&cfg.App.Env, "PPCLASS_ENV")
	setString(&cfg.HTTP.Addr, "PPCLASS_HTTP_ADDR")
	setDuration(&cfg.HTTP.ReadTimeout, "PPCLASS_HTTP_READ_TIMEOUT")
	setDuration(&cfg.HTTP.WriteTimeout, "PPCLASS_HTTP_WRITE_TIMEOUT")
	setDuration(&cfg.HTTP.ShutdownTimeout, "PPCLASS_HTTP_SHUTDOWN_TIMEOUT")
	if v := os.Getenv("PPCLASS_HTTP_CORS_ORIGINS"); v != "" {
		cfg.HTTP.CORSOrigins = splitCSV(v)
	}
	setString(&cfg.Log.Level, "PPCLASS_LOG_LEVEL")
	setString(&cfg.Log.Format, "PPCLASS_LOG_FORMAT")
	setString(&cfg.MySQL.DSN, "PPCLASS_MYSQL_DSN")
	setInt(&cfg.MySQL.MaxOpenConns, "PPCLASS_MYSQL_MAX_OPEN_CONNS")
	setInt(&cfg.MySQL.MaxIdleConns, "PPCLASS_MYSQL_MAX_IDLE_CONNS")
	setDuration(&cfg.MySQL.ConnMaxLifetime, "PPCLASS_MYSQL_CONN_MAX_LIFETIME")
	setDuration(&cfg.MySQL.ConnMaxIdleTime, "PPCLASS_MYSQL_CONN_MAX_IDLE_TIME")
	setBool(&cfg.MySQL.AutoMigrate, "PPCLASS_MYSQL_AUTO_MIGRATE")
	setString(&cfg.Auth.JWTSecret, "PPCLASS_JWT_SECRET")
	setString(&cfg.Auth.Issuer, "PPCLASS_AUTH_ISSUER")
	setDuration(&cfg.Auth.AccessTTL, "PPCLASS_AUTH_ACCESS_TTL")
	setDuration(&cfg.Classroom.InviteTTL, "PPCLASS_CLASSROOM_INVITE_TTL")
	setDuration(&cfg.Classroom.RoomTokenTTL, "PPCLASS_CLASSROOM_ROOM_TOKEN_TTL")
	setDuration(&cfg.Classroom.ConsentTTL, "PPCLASS_CLASSROOM_CONSENT_TTL")
	setDuration(&cfg.Classroom.IdleTTL, "PPCLASS_CLASSROOM_IDLE_TTL")
	setInt64(&cfg.Classroom.MaxFileBytes, "PPCLASS_CLASSROOM_MAX_FILE_BYTES")
	setInt64(&cfg.Classroom.MaxImageBytes, "PPCLASS_CLASSROOM_MAX_IMAGE_BYTES")
	setInt64(&cfg.Classroom.RoomQuotaBytes, "PPCLASS_CLASSROOM_ROOM_QUOTA_BYTES")
	setDuration(&cfg.Classroom.AttachmentRetention, "PPCLASS_CLASSROOM_ATTACHMENT_RETENTION")
	setInt(&cfg.Classroom.MessageRatePerMinute, "PPCLASS_CLASSROOM_MESSAGE_RATE_PER_MINUTE")
	setInt(&cfg.Classroom.UploadRatePerMinute, "PPCLASS_CLASSROOM_UPLOAD_RATE_PER_MINUTE")
	setInt(&cfg.Classroom.InviteRatePerMinute, "PPCLASS_CLASSROOM_INVITE_RATE_PER_MINUTE")
	setString(&cfg.Storage.DataDir, "PPCLASS_STORAGE_DATA_DIR")
	setString(&cfg.Storage.AttachmentDir, "PPCLASS_STORAGE_ATTACHMENT_DIR")
	setBool(&cfg.PPCDN.Enabled, "PPCLASS_PPCDN_ENABLED")
	setString(&cfg.PPCDN.BaseURL, "PPCLASS_PPCDN_BASE_URL")
	setString(&cfg.PPCDN.AppID, "PPCLASS_PPCDN_APP_ID")
	setString(&cfg.PPCDN.AppSecret, "PPCLASS_PPCDN_APP_SECRET")
	setString(&cfg.PPCDN.KeyID, "PPCLASS_PPCDN_KEY_ID")
	setString(&cfg.PPCDN.DeviceID, "PPCLASS_PPCDN_DEVICE_ID")
	setString(&cfg.PPCDN.Region, "PPCLASS_PPCDN_REGION")
	setDuration(&cfg.PPCDN.Timeout, "PPCLASS_PPCDN_TIMEOUT")
	setDuration(&cfg.PPCDN.MediaTTL, "PPCLASS_PPCDN_MEDIA_TTL")
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.App.Name) == "" {
		return fmt.Errorf("app.name is required")
	}
	if strings.TrimSpace(c.HTTP.Addr) == "" {
		return fmt.Errorf("http.addr is required")
	}
	if c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 || c.HTTP.ShutdownTimeout <= 0 {
		return fmt.Errorf("http timeouts must be positive")
	}
	if c.MySQL.DSN == "" {
		return fmt.Errorf("mysql.dsn is required")
	}
	if c.MySQL.MaxOpenConns <= 0 {
		return fmt.Errorf("mysql.maxOpenConns must be positive")
	}
	if c.MySQL.ConnMaxIdleTime < 0 {
		return fmt.Errorf("mysql.connMaxIdleTime must not be negative")
	}
	if c.MySQL.MaxIdleConns < 0 || c.MySQL.MaxIdleConns > c.MySQL.MaxOpenConns {
		return fmt.Errorf("mysql.maxIdleConns must be between 0 and maxOpenConns")
	}
	if c.Auth.Issuer == "" {
		return fmt.Errorf("auth.issuer is required")
	}
	if c.Auth.AccessTTL <= 0 {
		return fmt.Errorf("auth.accessTTL must be positive")
	}
	if c.Classroom.InviteTTL <= 0 || c.Classroom.RoomTokenTTL <= 0 {
		return fmt.Errorf("classroom inviteTTL and roomTokenTTL must be positive")
	}
	if c.Classroom.ConsentTTL <= 0 || c.Classroom.IdleTTL <= 0 {
		return fmt.Errorf("classroom consentTTL and idleTTL must be positive")
	}
	if c.Classroom.MaxFileBytes <= 0 || c.Classroom.MaxImageBytes <= 0 {
		return fmt.Errorf("classroom size limits must be positive")
	}
	if c.Classroom.RoomQuotaBytes < c.Classroom.MaxFileBytes {
		return fmt.Errorf("classroom roomQuotaBytes must be at least maxFileBytes")
	}
	if c.Classroom.AttachmentRetention <= 0 {
		return fmt.Errorf("classroom retention periods must be positive")
	}
	if c.Classroom.MessageRatePerMinute <= 0 || c.Classroom.UploadRatePerMinute <= 0 || c.Classroom.InviteRatePerMinute <= 0 {
		return fmt.Errorf("classroom rate limits must be positive")
	}
	if strings.TrimSpace(c.Storage.DataDir) == "" {
		return fmt.Errorf("storage.dataDir is required")
	}
	if c.PPCDN.Enabled {
		if strings.TrimSpace(c.PPCDN.BaseURL) == "" || strings.TrimSpace(c.PPCDN.AppID) == "" || strings.TrimSpace(c.PPCDN.AppSecret) == "" {
			return fmt.Errorf("ppcdn baseUrl, appId and appSecret are required when ppcdn is enabled")
		}
		if c.PPCDN.Timeout <= 0 || c.PPCDN.MediaTTL <= 0 {
			return fmt.Errorf("ppcdn timeout and mediaTTL must be positive")
		}
	}
	if strings.EqualFold(c.App.Env, "prod") {
		if len(c.Auth.JWTSecret) < 32 {
			return fmt.Errorf("auth.jwtSecret must be at least 32 characters in prod")
		}
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be one of debug, info, warn, error")
	}
	switch strings.ToLower(c.Log.Format) {
	case "text", "json":
	default:
		return fmt.Errorf("log.format must be text or json")
	}
	return nil
}

func setString(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func setDuration(dst *Duration, key string) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	parsed, err := time.ParseDuration(v)
	if err == nil {
		*dst = Duration(parsed)
	}
}

func setInt(dst *int, key string) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	parsed, err := strconv.Atoi(v)
	if err == nil {
		*dst = parsed
	}
}

func setInt64(dst *int64, key string) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err == nil {
		*dst = parsed
	}
}

func setBool(dst *bool, key string) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	parsed, err := strconv.ParseBool(v)
	if err == nil {
		*dst = parsed
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
