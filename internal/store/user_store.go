package store

import (
	"context"
	"strings"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

type UserStore struct {
	db *gorm.DB
}

func NewUserStore(db *gorm.DB) *UserStore {
	return &UserStore{db: db}
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	err := s.db.WithContext(ctx).Where("email = ?", NormalizeEmail(email)).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserStore) GetByID(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	err := s.db.WithContext(ctx).First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserStore) Create(ctx context.Context, user *model.User) error {
	user.Email = NormalizeEmail(user.Email)
	return s.db.WithContext(ctx).Create(user).Error
}

func (s *UserStore) Save(ctx context.Context, user *model.User) error {
	user.Email = NormalizeEmail(user.Email)
	return s.db.WithContext(ctx).Save(user).Error
}

func (s *UserStore) ListByIDs(ctx context.Context, ids []uint64) ([]model.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var users []model.User
	err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error
	return users, err
}

func (s *UserStore) UpdateFields(ctx context.Context, id uint64, fields map[string]any) error {
	return s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Updates(fields).Error
}

func (s *UserStore) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&model.User{}).
		Where("role = ? AND status = ?", model.RoleAdmin, model.StatusActive).
		Count(&count).Error
	return count, err
}
