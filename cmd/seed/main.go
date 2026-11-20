package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/config"
	"ppclassvip/internal/database"
	"ppclassvip/internal/logger"
	"ppclassvip/internal/model"
	"ppclassvip/internal/store"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	configPath := flag.String("config", "conf/config.dev.yaml", "path to config file")
	email := flag.String("email", "", "teacher/admin email")
	password := flag.String("password", "", "password (at least 8 characters)")
	nickname := flag.String("nickname", "", "display name")
	role := flag.String("role", model.RoleTeacher, "role: teacher or admin")
	flag.Parse()

	_ = godotenv.Load()

	if strings.TrimSpace(*email) == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "-email and -password are required")
		os.Exit(2)
	}
	if *role != model.RoleTeacher && *role != model.RoleAdmin {
		fmt.Fprintln(os.Stderr, "-role must be teacher or admin")
		os.Exit(2)
	}
	if err := auth.ValidatePassword(*password); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(2)
	}
	log := logger.New(cfg.Log)

	db, err := database.Open(cfg.MySQL, log)
	if err != nil {
		log.Error("database init failed", "error", err)
		os.Exit(3)
	}
	defer func() {
		if err := database.Close(db); err != nil {
			log.Warn("close database failed", "error", err)
		}
	}()

	if err := database.AutoMigrate(db, model.AllModels()...); err != nil {
		log.Error("database migrate failed", "error", err)
		os.Exit(4)
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Error("hash password failed", "error", err)
		os.Exit(5)
	}

	users := store.NewUserStore(db)
	ctx := context.Background()

	user, err := users.GetByEmail(ctx, *email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Error("query user failed", "error", err)
		os.Exit(6)
	}
	if user == nil {
		user = &model.User{
			Email:        store.NormalizeEmail(*email),
			PasswordHash: hash,
			Nickname:     strings.TrimSpace(*nickname),
			Role:         *role,
			Status:       model.StatusActive,
		}
		if user.Nickname == "" {
			user.Nickname = strings.Split(user.Email, "@")[0]
		}
		if err := users.Create(ctx, user); err != nil {
			log.Error("create user failed", "error", err)
			os.Exit(7)
		}
		log.Info("user created", "id", user.ID, "email", user.Email, "role", user.Role)
		return
	}

	user.PasswordHash = hash
	user.Role = *role
	user.Status = model.StatusActive
	if strings.TrimSpace(*nickname) != "" {
		user.Nickname = strings.TrimSpace(*nickname)
	}
	if err := users.Save(ctx, user); err != nil {
		log.Error("update user failed", "error", err)
		os.Exit(8)
	}
	log.Info("user updated", "id", user.ID, "email", user.Email, "role", user.Role)
}
