package ffmpeg

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type SceneDetector struct {
	ffmpegPath  string
	ffprobePath string
}

func NewSceneDetector(ffmpegPath, ffprobePath string) *SceneDetector {
	return &SceneDetector{
		ffmpegPath:  ffmpegPath,
		ffprobePath: ffprobePath,
	}
}

// GetVideoDuration возвращает длительность видео в секундах
func (d *SceneDetector) GetVideoDuration(inputPath string) (float64, error) {
	cmd := exec.Command(d.ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)

	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return 0, fmt.Errorf("failed to get video duration: %w", err)
	}

	durationStr := strings.TrimSpace(out.String())
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

// GetVideoFPS возвращает FPS видео
func (d *SceneDetector) GetVideoFPS(inputPath string) (float64, error) {
	cmd := exec.Command(d.ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)

	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to get fps: %w", err)
	}

	fpsStr := strings.TrimSpace(out.String())
	// Парсим дробь типа "30000/1001"
	parts := strings.Split(fpsStr, "/")
	if len(parts) == 2 {
		num, _ := strconv.ParseFloat(parts[0], 64)
		den, _ := strconv.ParseFloat(parts[1], 64)
		if den > 0 {
			return num / den, nil
		}
	}

	fps, _ := strconv.ParseFloat(fpsStr, 64)
	if fps == 0 {
		fps = 24.0 // значение по умолчанию
	}
	return fps, nil
}

// ExtractFramesWithInterval извлекает кадры с заданным интервалом (например, каждый 24-й)
func (d *SceneDetector) ExtractFramesWithInterval(inputPath string, frameInterval int, outputDir string) ([]string, []float64, error) {
	// Создаем выходную директорию
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create output dir: %w", err)
	}

	// Получаем FPS для расчета временных меток
	fps, err := d.GetVideoFPS(inputPath)
	if err != nil {
		fps = 24.0
	}

	// Извлекаем каждый frameInterval-й кадр
	// filter="select='not(mod(n,{interval}))'" выбирает каждый N-й кадр
	outputPattern := filepath.Join(outputDir, "frame_%06d.jpg")

	cmd := exec.Command(d.ffmpegPath,
		"-i", inputPath,
		"-vf", fmt.Sprintf("select='not(mod(n,%d))',setpts=N/FRAME_RATE/TB", frameInterval),
		"-vsync", "0",
		"-frame_pts", "1",
		"-q:v", "2",
		"-y",
		outputPattern,
	)

	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("failed to extract frames: %w", err)
	}

	// Собираем пути к кадрам и их временные метки
	framePaths := make([]string, 0)
	timestamps := make([]float64, 0)

	// Читаем созданные файлы
	files, err := filepath.Glob(filepath.Join(outputDir, "frame_*.jpg"))
	if err != nil {
		return nil, nil, err
	}

	// Сортируем файлы по номеру
	for i := 1; i <= len(files); i++ {
		framePath := filepath.Join(outputDir, fmt.Sprintf("frame_%06d.jpg", i))
		if _, err := os.Stat(framePath); err == nil {
			framePaths = append(framePaths, framePath)
			// Временная метка = (номер кадра * интервал) / FPS
			timestamp := float64(i*frameInterval) / fps
			timestamps = append(timestamps, timestamp)
		}
	}

	return framePaths, timestamps, nil
}

// DetectScenesWithInterval детектирует сцены, анализируя каждый N-й кадр
func (d *SceneDetector) DetectScenesWithInterval(inputPath string, threshold float64, frameInterval int) ([]float64, error) {
	if threshold == 0 {
		threshold = 0.15 // По умолчанию ниже, так как анализируем реже
	}

	// Берем каждый N-й кадр и на них применяем scene detection
	cmd := exec.Command(d.ffmpegPath,
		"-i", inputPath,
		"-vf", fmt.Sprintf("select='not(mod(n,%d))',select='gt(scene,%.2f)',showinfo", frameInterval, threshold),
		"-f", "null",
		"-",
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	scanner := bufio.NewScanner(stderr)
	sceneTimes := []float64{0} // начало видео
	re := regexp.MustCompile(`pts_time:([0-9.]+)`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			time, err := strconv.ParseFloat(matches[1], 64)
			if err == nil && time > 0 {
				sceneTimes = append(sceneTimes, time)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		fmt.Printf("ffmpeg warning: %v\n", err)
	}

	return sceneTimes, nil
}

// ExtractAudio извлекает аудиодорожку для сцены
func (d *SceneDetector) ExtractAudio(inputPath string, startTime, endTime float64, outputPath string) error {
	duration := endTime - startTime
	if duration <= 0 {
		return fmt.Errorf("invalid duration: %f", duration)
	}

	cmd := exec.Command(d.ffmpegPath,
		"-i", inputPath,
		"-ss", fmt.Sprintf("%.3f", startTime),
		"-t", fmt.Sprintf("%.3f", duration),
		"-q:a", "0",
		"-map", "a",
		"-y",
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to extract audio: %w", err)
	}

	return nil
}

// ExtractFrame (оставляем для совместимости)
func (d *SceneDetector) ExtractFrame(inputPath string, timestamp float64, outputPath string) error {
	cmd := exec.Command(d.ffmpegPath,
		"-i", inputPath,
		"-ss", fmt.Sprintf("%.3f", timestamp),
		"-vframes", "1",
		"-q:v", "2",
		"-y",
		outputPath,
	)

	return cmd.Run()
}
