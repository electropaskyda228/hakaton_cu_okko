package ffmpeg

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type SceneDetector struct {
	ffmpegPath  string
	ffprobePath string
}

type SceneInfo struct {
	StartTime float64
	EndTime   float64
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

// DetectScenes возвращает временные метки начала сцен
// threshold - чувствительность детекции (0.1-0.5, меньше = больше сцен)
func (d *SceneDetector) DetectScenes(inputPath string, threshold float64) ([]float64, error) {
	if threshold == 0 {
		threshold = 0.3 // значение по умолчанию
	}

	// Используем filter: select='gt(scene,threshold)', showinfo
	cmd := exec.Command(d.ffmpegPath,
		"-i", inputPath,
		"-vf", fmt.Sprintf("select='gt(scene,%.2f)',showinfo", threshold),
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

	// Парсим stderr для поиска pts_time
	scanner := bufio.NewScanner(stderr)
	sceneTimes := make([]float64, 0)
	// Добавляем начало видео как первую сцену
	sceneTimes = append(sceneTimes, 0)

	// Регулярное выражение для поиска pts_time
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
		// FFmpeg может завершиться с ошибкой, но данные могли быть получены
		// Поэтому просто логируем, но не прерываем
		fmt.Printf("ffmpeg warning: %v\n", err)
	}

	return sceneTimes, nil
}

// ExtractFrame извлекает один кадр в заданный момент времени
func (d *SceneDetector) ExtractFrame(inputPath string, timestamp float64, outputPath string) error {
	cmd := exec.Command(d.ffmpegPath,
		"-i", inputPath,
		"-ss", fmt.Sprintf("%.3f", timestamp),
		"-vframes", "1",
		"-q:v", "2", // высокое качество
		"-y", // перезаписывать
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to extract frame: %w", err)
	}

	return nil
}

// ExtractSceneFrames извлекает кадры для всех сцен
func (d *SceneDetector) ExtractSceneFrames(inputPath string, sceneTimes []float64, outputDir string) ([]string, error) {
	framePaths := make([]string, 0, len(sceneTimes))

	for i, time := range sceneTimes {
		outputPath := fmt.Sprintf("%s/scene_%03d_%.0f.jpg", outputDir, i, time)
		if err := d.ExtractFrame(inputPath, time, outputPath); err != nil {
			return nil, fmt.Errorf("failed to extract frame %d: %w", i, err)
		}
		framePaths = append(framePaths, outputPath)
	}

	return framePaths, nil
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
		"-q:a", "0", // лучшее качество аудио
		"-map", "a", // только аудиодорожка
		"-y", // перезаписывать
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to extract audio: %w", err)
	}

	return nil
}

// ExtractFrameWithInterval извлекает кадр с учетом интервала (каждый 24-й кадр)
func (d *SceneDetector) ExtractFrameWithInterval(inputPath string, timestamp float64, frameInterval int, outputPath string) error {
	// Для каждого 24-го кадра мы все равно извлекаем один кадр, но на входе
	// мы уже получили timestamp, который соответствует нужному кадру
	cmd := exec.Command(d.ffmpegPath,
		"-i", inputPath,
		"-ss", fmt.Sprintf("%.3f", timestamp),
		"-vframes", "1",
		"-q:v", "2",
		"-y",
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to extract frame: %w", err)
	}

	return nil
}

// GetVideoInfo возвращает информацию о видео (FPS, количество кадров)
func (d *SceneDetector) GetVideoInfo(inputPath string) (fps float64, frameCount int, err error) {
	// Получаем FPS
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
		return 0, 0, fmt.Errorf("failed to get fps: %w", err)
	}

	fpsStr := strings.TrimSpace(out.String())
	// Парсим дробь типа "30000/1001"
	parts := strings.Split(fpsStr, "/")
	if len(parts) == 2 {
		num, _ := strconv.ParseFloat(parts[0], 64)
		den, _ := strconv.ParseFloat(parts[1], 64)
		if den > 0 {
			fps = num / den
		}
	} else {
		fps, _ = strconv.ParseFloat(fpsStr, 64)
	}

	// Получаем длительность
	duration, err := d.GetVideoDuration(inputPath)
	if err != nil {
		return fps, 0, err
	}

	frameCount = int(duration * fps)
	return fps, frameCount, nil
}
