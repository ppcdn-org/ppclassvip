package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	TokenTypeAccess        = "access"
	TokenTypeRoom          = "room"
	TokenTypeRemoteControl = "remote_control"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	UserID        uint64 `json:"uid,omitempty"`
	RoomID        uint64 `json:"rid,omitempty"`
	ParticipantID uint64 `json:"pid,omitempty"`
	Role          string `json:"role"`
	SessionID     string `json:"sid,omitempty"`
	Type          string `json:"typ"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

func NewTokenManager(secret, issuer string, ttl time.Duration) (*TokenManager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("jwt secret must be at least 32 characters")
	}
	if issuer == "" {
		return nil, fmt.Errorf("jwt issuer is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("jwt ttl must be positive")
	}
	return &TokenManager{
		secret: []byte(secret),
		issuer: issuer,
		ttl:    ttl,
	}, nil
}

func (m *TokenManager) TTL() time.Duration {
	return m.ttl
}

func (m *TokenManager) Issue(userID uint64, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		Type:   TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(userID, 10),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *TokenManager) IssueRoom(roomID, participantID uint64, role string, ttl time.Duration) (string, error) {
	if roomID == 0 || participantID == 0 {
		return "", fmt.Errorf("roomID and participantID are required")
	}
	if ttl <= 0 {
		ttl = m.ttl
	}
	now := time.Now()
	claims := Claims{
		RoomID:        roomID,
		ParticipantID: participantID,
		Role:          role,
		Type:          TokenTypeRoom,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("room:%d:participant:%d", roomID, participantID),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *TokenManager) IssueRemoteControl(sessionID string, roomID, participantID uint64, role string, ttl time.Duration) (string, error) {
	if sessionID == "" || roomID == 0 || participantID == 0 {
		return "", fmt.Errorf("sessionID, roomID and participantID are required")
	}
	if ttl <= 0 {
		ttl = m.ttl
	}
	now := time.Now()
	claims := Claims{
		RoomID:        roomID,
		ParticipantID: participantID,
		SessionID:     sessionID,
		Role:          role,
		Type:          TokenTypeRemoteControl,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("room:%d:session:%s:%s", roomID, sessionID, role),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *TokenManager) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(
		token,
		claims,
		func(t *jwt.Token) (any, error) {
			if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, ErrInvalidToken
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
	)
	if err != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	switch claims.Type {
	case TokenTypeAccess:
		if claims.UserID == 0 {
			return nil, ErrInvalidToken
		}
	case TokenTypeRoom:
		if claims.RoomID == 0 || claims.ParticipantID == 0 {
			return nil, ErrInvalidToken
		}
	case TokenTypeRemoteControl:
		if claims.RoomID == 0 || claims.ParticipantID == 0 || claims.SessionID == "" {
			return nil, ErrInvalidToken
		}
		if claims.Role != "controller" && claims.Role != "agent" {
			return nil, ErrInvalidToken
		}
	default:
		return nil, ErrInvalidToken
	}
	return claims, nil
}
