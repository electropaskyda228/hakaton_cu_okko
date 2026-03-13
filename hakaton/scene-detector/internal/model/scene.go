package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Scene struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	VideoName        string    `gorm:"not null" json:"video_name"`
	SceneNumber      int       `gorm:"not null" json:"scene_number"`
	StartTimeSeconds float64   `gorm:"not null" json:"start_time_seconds"`
	EndTimeSeconds   float64   `gorm:"not null" json:"end_time_seconds"`
	FrameURL         string    `gorm:"not null;size:1000" json:"frame_url"`
	CreatedAt        time.Time `gorm:"not null" json:"created_at"`
}

// Hook для генерации UUID если default не сработает
func (s *Scene) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	return nil
}
