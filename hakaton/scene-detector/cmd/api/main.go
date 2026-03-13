package main

import (
	"log"
	"scene-detector/internal/config"
	"scene-detector/internal/handler"
	"scene-detector/internal/model"
	"scene-detector/internal/repository"
	"scene-detector/pkg/ffmpeg"
	minioPkg "scene-detector/pkg/minio"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// Загружаем конфигурацию
	cfg := config.Load()

	// Подключаемся к PostgreSQL
	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Автомиграция
	if err := db.AutoMigrate(&model.Scene{}); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// Создаем репозиторий
	sceneRepo := repository.NewSceneRepository(db)

	// Подключаемся к MinIO
	minioClient, err := minioPkg.NewClient(
		cfg.MinIOEndpoint,
		cfg.MinIOAccessKey,
		cfg.MinIOSecretKey,
		cfg.MinIOBucket,
		cfg.MinIOUseSSL,
	)
	if err != nil {
		log.Fatalf("Failed to connect to MinIO: %v", err)
	}

	// Создаем детектор сцен
	sceneDetector := ffmpeg.NewSceneDetector(cfg.FFmpegPath, cfg.FFprobePath)

	// Создаем обработчик
	videoHandler := handler.NewVideoHandler(sceneRepo, sceneDetector, minioClient)

	// Настраиваем роутер
	r := gin.Default()

	// Ручки API
	api := r.Group("/api/v1")
	{
		api.POST("/video/process", videoHandler.ProcessVideo)
		api.GET("/scenes", videoHandler.GetScenes)
	}

	// Запускаем сервер
	log.Println("Server starting on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
