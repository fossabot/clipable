package transcoder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"webserver/models"
	"webserver/services"

	"github.com/alitto/pond"
	cmap "github.com/orcaman/concurrent-map/v2"
	log "github.com/sirupsen/logrus"
	"github.com/volatiletech/sqlboiler/v4/boil"
)

// A go package that implements a worker pool to process files in minio using ffmpeg into mpeg-dash format
// and stores the output in minio.
type transcoder struct {
	*services.Group
	pool *pond.WorkerPool

	progress cmap.ConcurrentMap[int64, int]
	started  bool
}

func New(grp *services.Group, workers int) (services.Transcoder, error) {
	t := &transcoder{
		pool:  pond.New(workers, 1000),
		Group: grp,
		progress: cmap.NewWithCustomShardingFunction[int64, int](func(key int64) uint32 {
			// Copilot recommended this i have no idea if its correct
			return uint32(key % 10)
		}),
		started: false,
	}

	// Find all clips that are marked as processing while starting to resume their processing
	orphanedClips, err := t.Clips.FindMany(context.Background(), models.ClipWhere.Processing.EQ(true))

	if err != nil {
		return nil, err
	}

	for _, clip := range orphanedClips {
		if err := t.Queue(context.Background(), clip); err != nil {
			return nil, err
		}
	}

	return t, nil
}

func (t *transcoder) Start() error {
	if t.started {
		return fmt.Errorf("transcoder already started")
	}

	t.started = true

	return nil
}

func (t *transcoder) Queue(ctx context.Context, clip *models.Clip) error {
	t.progress.Set(clip.ID, -1)

	t.pool.Submit(func() {
		defer func() {
			if v := recover(); v != nil {
				log.WithField("clip", clip.ID).
					WithField("panic", v).
					Error("Panic in transcoder")
			}
		}()
		t.process(ctx, clip)
	})

	return nil
}

func (t *transcoder) GetProgress(clipID int64) (int, bool) {
	return t.progress.Get(clipID)
}

func (t *transcoder) reportProgress(pipe io.ReadCloser, clip *models.Clip, duration time.Duration) {
	defer pipe.Close()
	t.progress.Set(clip.ID, 0)

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		// Example line: frame=  100 fps=0.0 q=-1.0 size=     128kB time=00:00:03.00 bitrate= 341.0kbits/s speed=1.01e+03x
		line := scanner.Text()
		if strings.Contains(line, "time=") {
			suffixPart := strings.Split(line, "time=")[1]
			timeString := strings.Split(suffixPart, "bitrate=")[0]

			currentTime, err := ParseSexagesimal(timeString)
			if err != nil {
				log.WithError(err).Error("Failed to parse frame number")
				return
			}

			// Calculate the progress
			progress := math.Round(float64(currentTime) / float64(duration) * 100.0)
			t.progress.Set(clip.ID, int(progress))
		}
	}

	t.progress.Remove(clip.ID)
}

func (t *transcoder) process(ctx context.Context, clip *models.Clip) {
	// Maybe just use https://stackoverflow.com/questions/53352348/mpeg-dash-output-generated-by-ffmpeg-not-working ?
	// Example of variables in ffmpeg https://ottverse.com/hls-packaging-using-ffmpeg-live-vod/
	// Example of using ffmpeg map to pipes https://stackoverflow.com/questions/71041370/separate-video-from-audio-from-ffmpeg-stream
	// syscall pipe: https://www.codeflict.com/go/syscall-pipe
	// https://support.google.com/youtube/answer/1722171?hl=en#zippy=%2Cbitrate

	// Wait until we're started listen for requests before attempting to launch ffmpeg
	for !t.started {
		time.Sleep(1 * time.Second)
	}

	log.Infoln("Transcoding video", clip.ID)

	rawURL := fmt.Sprintf("http://127.0.0.1:12786/s3/%d/raw", clip.ID)

	cmd := exec.Command("ffmpeg",
		"-i", rawURL,
		"-ss", "00:00:01",
		"-s", "1280x720",
		"-qscale:v", "5",
		"-frames:v", "1",
		fmt.Sprintf("http://127.0.0.1:12786/s3/%d/thumbnail.jpg", clip.ID),
	)

	_, err := cmd.CombinedOutput()

	if err != nil {
		log.WithError(err).
			Error(cmd.String())
		return
	}

	width, height, fps, duration, audioStreams, err := GetVideoStats(rawURL)

	if err != nil {
		log.WithError(err).
			Error("Error getting video stats")
		return
	}

	fmt.Println("Width", width, "Height", height, "FPS", fps, "Duration", duration, "AudioStreams", audioStreams)

	ffmpegArgs := []string{
		"-i", rawURL,
		"-preset", "veryslow",
		"-keyint_min", strconv.Itoa(fps),
		"-hls_playlist_type", "vod",
		"-g", strconv.Itoa(fps),
		"-sc_threshold", "0",
		"-seg_duration", "1",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "1",
		"-ar", "96000",
		"-use_template", "1",
		"-use_timeline", "1",
		"-single_file", "1",
		"-tune", "film",
		"-x264opts", "no-scenecut",
		"-streaming", "0",
		"-movflags", "frag_keyframe+empty_moov",
		"-utc_timing_url", "https://time.akamai.com/?iso",
	}

	ffmpegArgs = append(ffmpegArgs, GetPresets(width, height, fps, audioStreams)...)

	if audioStreams > 0 {
		ffmpegArgs = append(ffmpegArgs, "-map", "0:a")
	}

	if audioStreams > 1 {
		ffmpegArgs = append(
			ffmpegArgs,
			"-filter_complex",
			"amerge=inputs="+strconv.Itoa(audioStreams),
		)
	}

	if audioStreams > 0 {
		ffmpegArgs = append(ffmpegArgs, "-adaptation_sets", "id=0,streams=v id=1,streams=a")
	} else {
		ffmpegArgs = append(ffmpegArgs, "-adaptation_sets", "id=0,streams=v")
	}

	ffmpegArgs = append(ffmpegArgs,
		"-f", "dash",
		fmt.Sprintf("http://127.0.0.1:12786/s3/%d/dash.mpd", clip.ID),
	)

	cmd = exec.Command("ffmpeg", ffmpegArgs...)

	stderr, err := cmd.StderrPipe()

	if err != nil {
		log.WithError(err).
			Error(cmd.String())
		return
	}

	// Get the progress of the transcoding from ffmpeg's stderr
	go t.reportProgress(stderr, clip, duration)

	err = cmd.Start()

	if err != nil {
		log.WithError(err).
			Error(cmd.String())
		return
	}

	err = cmd.Wait()

	if err != nil {
		log.WithError(err).
			Error(cmd.String())
		return
	}

	log.Infoln("Finished transcoding video", clip.ID)

	if err := t.ObjectStore.DeleteObject(ctx, fmt.Sprintf("%d/raw", clip.ID)); err != nil {
		log.WithError(err).
			Error("Error deleting raw video")
		return
	}

	clip.Processing = false

	if err := t.Clips.Update(ctx, clip, boil.Whitelist(models.ClipColumns.Processing)); err != nil {
		log.WithError(err).
			Error("Error updating clip")
		return
	}
}
