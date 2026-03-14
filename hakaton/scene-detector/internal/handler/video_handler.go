package handler

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"scene-detector/internal/model"
	"scene-detector/internal/repository"
	"scene-detector/pkg/ffmpeg"
	minioClient "scene-detector/pkg/minio"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type VideoHandler struct {
	sceneRepo     repository.SceneRepository
	sceneDetector *ffmpeg.SceneDetector
	minioClient   *minioClient.Client
	tempDir       string
}

func NewVideoHandler(
	sceneRepo repository.SceneRepository,
	sceneDetector *ffmpeg.SceneDetector,
	minioClient *minioClient.Client,
) *VideoHandler {
	// Создаем временную директорию
	tempDir := filepath.Join(os.TempDir(), "scene-detector")
	os.MkdirAll(tempDir, 0755)

	return &VideoHandler{
		sceneRepo:     sceneRepo,
		sceneDetector: sceneDetector,
		minioClient:   minioClient,
		tempDir:       tempDir,
	}
}

type SceneResponse struct {
	SceneID     string  `json:"scene_id"`
	SceneNumber int     `json:"scene_number"`
	FrameURL    string  `json:"frame_url"`
	AudioURL    string  `json:"audio_url,omitempty"`
	StartTime   float64 `json:"start_time"`
	EndTime     float64 `json:"end_time"`
}

type ProcessVideoRequest struct {
	Threshold float64 `form:"threshold" binding:"omitempty,min=0.1,max=0.9"`
}

func (h *VideoHandler) ProcessVideo(c *gin.Context) {
	var req ProcessVideoRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	videoPath := filepath.Join(h.tempDir, uuid.New().String()+"_"+header.Filename)
	videoFile, err := os.Create(videoPath)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to create temp file"})
		return
	}
	defer os.Remove(videoPath)

	if _, err := io.Copy(videoFile, file); err != nil {
		videoFile.Close()
		c.JSON(500, gin.H{"error": "failed to save file"})
		return
	}
	videoFile.Close()

	// Получаем FPS
	fps, err := h.sceneDetector.GetVideoFPS(videoPath)
	if err != nil {
		fps = 24.0
	}

	// Будем анализировать каждый 24-й кадр
	frameInterval := int(fps) // 24 при 24 fps

	// Детектируем сцены, анализируя только каждый 24-й кадр (БЫСТРО!)
	threshold := req.Threshold
	if threshold == 0 {
		threshold = 0.3
	}

	sceneTimes, err := h.sceneDetector.DetectScenesWithInterval(videoPath, threshold, frameInterval)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("failed to detect scenes: %v", err)})
		return
	}

	// Получаем длительность видео
	duration, err := h.sceneDetector.GetVideoDuration(videoPath)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("failed to get video duration: %v", err)})
		return
	}

	// Создаем временную директорию для кадров
	framesDir := filepath.Join(h.tempDir, "frames_"+uuid.New().String())
	if err := os.MkdirAll(framesDir, 0755); err != nil {
		c.JSON(500, gin.H{"error": "failed to create frames dir"})
		return
	}
	defer os.RemoveAll(framesDir)

	// Извлекаем кадры для каждой сцены
	scenes := make([]SceneResponse, 0, len(sceneTimes))

	for i, startTime := range sceneTimes {
		// Определяем конец сцены
		var endTime float64
		if i < len(sceneTimes)-1 {
			endTime = sceneTimes[i+1]
		} else {
			endTime = duration
		}

		// Берем кадр из середины сцены (лучше представляет сцену)
		middleTime := startTime + (endTime-startTime)/2

		// Создаем временный файл для кадра
		framePath := filepath.Join(h.tempDir, fmt.Sprintf("frame_%d_%s.jpg", i, uuid.New().String()))

		// Извлекаем кадр
		if err := h.sceneDetector.ExtractFrame(videoPath, middleTime, framePath); err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to extract frame %d: %v", i, err)})
			return
		}

		// Открываем файл для загрузки в MinIO
		frameFile, err := os.Open(framePath)
		if err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to open frame %d: %v", i, err)})
			return
		}

		frameInfo, err := frameFile.Stat()
		if err != nil {
			frameFile.Close()
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to get frame info %d: %v", i, err)})
			return
		}

		// Загружаем в MinIO
		objectName := fmt.Sprintf("scenes/%s/scene_%d_%s.jpg",
			header.Filename,
			i,
			uuid.New().String())

		frameURL, err := h.minioClient.UploadFile(
			c.Request.Context(),
			frameFile,
			frameInfo.Size(),
			"image/jpeg",
			objectName,
		)
		frameFile.Close()
		os.Remove(framePath)

		if err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to upload frame %d: %v", i, err)})
			return
		}

		// Извлекаем аудио для сцены (опционально)
		audioURL := ""
		audioPath := filepath.Join(h.tempDir, fmt.Sprintf("audio_%d_%s.mp3", i, uuid.New().String()))
		if err := h.sceneDetector.ExtractAudio(videoPath, startTime, endTime, audioPath); err == nil {
			audioFile, _ := os.Open(audioPath)
			audioInfo, _ := audioFile.Stat()

			audioObjectName := fmt.Sprintf("scenes/%s/audio/scene_%d_%s.mp3",
				header.Filename,
				i,
				uuid.New().String())

			audioURL, _ = h.minioClient.UploadFile(
				c.Request.Context(),
				audioFile,
				audioInfo.Size(),
				"audio/mpeg",
				audioObjectName,
			)
			audioFile.Close()
			os.Remove(audioPath)
		}

		// Сохраняем метаданные в БД
		scene := &model.Scene{
			VideoName:        header.Filename,
			SceneNumber:      i + 1,
			StartTimeSeconds: startTime,
			EndTimeSeconds:   endTime,
			FrameURL:         frameURL,
			AudioURL:         audioURL,
		}

		if err := h.sceneRepo.Create(scene); err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to save scene %d: %v", i, err)})
			return
		}

		scenes = append(scenes, SceneResponse{
			SceneID:     scene.ID,
			SceneNumber: scene.SceneNumber,
			FrameURL:    scene.FrameURL,
			AudioURL:    scene.AudioURL,
			StartTime:   scene.StartTimeSeconds,
			EndTime:     scene.EndTimeSeconds,
		})
	}

	c.JSON(200, gin.H{
		"message":      "video processed successfully",
		"scenes_count": len(scenes),
		"scenes":       scenes,
	})
}

func (h *VideoHandler) GetScenes(c *gin.Context) {
	videoName := c.Query("video_name")

	var scenes []model.Scene
	var err error

	if videoName != "" {
		scenes, err = h.sceneRepo.FindByVideoName(videoName)
	} else {
		scenes, err = h.sceneRepo.FindAll()
	}

	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, scenes)
}
