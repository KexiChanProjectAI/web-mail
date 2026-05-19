package storage

import (
	"bytes"
	"testing"
)

func TestSaveRawMIMEAndReadRawMIMERoundTrip(t *testing.T) {
	s, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	want := []byte("From: sender@example.com\n\nbody")
	if err := s.SaveRawMIME("abc123", want); err != nil {
		t.Fatalf("SaveRawMIME: %v", err)
	}
	got, err := s.ReadRawMIME("abc123")
	if err != nil {
		t.Fatalf("ReadRawMIME: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadRawMIME = %q, want %q", got, want)
	}
}

func TestSaveAttachmentAndReadAttachmentRoundTrip(t *testing.T) {
	s, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	want := []byte("attachment bytes")
	if err := s.SaveAttachment(42, "def456", want); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	got, err := s.ReadAttachment(42, "def456")
	if err != nil {
		t.Fatalf("ReadAttachment: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadAttachment = %q, want %q", got, want)
	}
}

func TestSaveRawMIMEIdempotent(t *testing.T) {
	s, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	if err := s.SaveRawMIME("abc123", []byte("first")); err != nil {
		t.Fatalf("first SaveRawMIME: %v", err)
	}
	if err := s.SaveRawMIME("abc123", []byte("second")); err != nil {
		t.Fatalf("second SaveRawMIME: %v", err)
	}
	got, err := s.ReadRawMIME("abc123")
	if err != nil {
		t.Fatalf("ReadRawMIME: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("raw MIME overwritten: got %q", got)
	}
}
