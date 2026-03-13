package repository

import (
	"scene-detector/internal/model"

	"gorm.io/gorm"
)

type SceneRepository interface {
	Create(scene *model.Scene) error
	FindByVideoName(videoName string) ([]model.Scene, error)
	FindAll() ([]model.Scene, error)
}

type sceneRepository struct {
	db *gorm.DB
}

func NewSceneRepository(db *gorm.DB) SceneRepository {
	return &sceneRepository{db: db}
}

func (r *sceneRepository) Create(scene *model.Scene) error {
	return r.db.Create(scene).Error
}

func (r *sceneRepository) FindByVideoName(videoName string) ([]model.Scene, error) {
	var scenes []model.Scene
	err := r.db.Where("video_name = ?", videoName).Order("scene_number").Find(&scenes).Error
	return scenes, err
}

func (r *sceneRepository) FindAll() ([]model.Scene, error) {
	var scenes []model.Scene
	err := r.db.Find(&scenes).Error
	return scenes, err
}
