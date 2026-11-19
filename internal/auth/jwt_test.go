package auth

import (
	"strings"
	"testing"
	"time"
)

func TestIssueAndParse(t *testing.T) {
	manager, err := NewTokenManager(strings.Repeat("a", 32), "ppclassvip", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	token, err := manager.Issue(42, "teacher")
	if err != nil {
		t.Fatal(err)
	}

	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 42 {
		t.Fatalf("UserID = %d", claims.UserID)
	}
	if claims.Role != "teacher" {
		t.Fatalf("Role = %q", claims.Role)
	}
	if claims.Type != TokenTypeAccess {
		t.Fatalf("Type = %q", claims.Type)
	}
}

func TestIssueAndParseRoomToken(t *testing.T) {
	manager, err := NewTokenManager(strings.Repeat("a", 32), "ppclassvip", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	token, err := manager.IssueRoom(7, 11, "student", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Type != TokenTypeRoom {
		t.Fatalf("Type = %q", claims.Type)
	}
	if claims.RoomID != 7 || claims.ParticipantID != 11 {
		t.Fatalf("room/participant = %d/%d", claims.RoomID, claims.ParticipantID)
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	issuer, _ := NewTokenManager(strings.Repeat("a", 32), "ppclassvip", time.Hour)
	verifier, _ := NewTokenManager(strings.Repeat("b", 32), "ppclassvip", time.Hour)

	token, err := issuer.Issue(1, "teacher")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Parse(token); err == nil {
		t.Fatal("expected invalid token")
	}
}

func TestNewTokenManagerRejectsShortSecret(t *testing.T) {
	if _, err := NewTokenManager("short", "ppclassvip", time.Hour); err == nil {
		t.Fatal("expected error")
	}
}
