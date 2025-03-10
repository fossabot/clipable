package routes

import (
	"context"
	"database/sql"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"webserver/models"
	"webserver/modelsx"

	"github.com/friendsofgo/errors"
	"github.com/volatiletech/sqlboiler/v4/boil"
)

func (r *Routes) UploadClip(user *models.User, req *http.Request) (int, []byte, error) {
	if user == nil {
		return http.StatusUnauthorized, nil, nil
	}

	// Get media type information from the content type header
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))

	if err != nil {
		log.Fatal(err)
	}

	// Check if the media type is multipart
	if !strings.HasPrefix(mediaType, "multipart/") {
		return http.StatusBadRequest, []byte("Content-Type is not multipart"), nil
	}

	mr := multipart.NewReader(req.Body, params["boundary"])

	// Get the first part, which should be the json
	json, err := mr.NextPart()

	if err == io.EOF {
		return http.StatusBadRequest, []byte("No json part"), nil
	}

	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if json.FormName() != "json" {
		return http.StatusBadRequest, []byte("First part must be json"), nil
	}

	// Parse the json into a clip
	clip, err := modelsx.ParseClip(json)

	if err != nil {
		return http.StatusBadRequest, []byte(err.Error()), nil
	}

	// Get the second part, which should be the video
	videoPart, err := mr.NextPart()

	if err == io.EOF {
		return http.StatusBadRequest, []byte("No video part"), nil
	}

	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	// Check if the video part is a video
	if videoPart.FormName() != "video" {
		return http.StatusBadRequest, []byte("Second part must be video"), nil
	}

	model := clip.ToModel()

	// Create the clip
	tx, err := r.Clips.Create(req.Context(), model, user, boil.Whitelist(clip.GetUpdateWhitelist()...))

	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	// Always attempt to rollback, even if it succeeds, if the tx is committed, this is a no-op
	defer tx.Rollback()

	len, err := tx.UploadVideo(req.Context(), io.LimitReader(videoPart, r.cfg.MaxUploadSizeBytes))

	// LimitReader will return io.EOF once the limit is reached, so if we read exactly our limit
	// there was more data to read, and the video was too large
	if len == r.cfg.MaxUploadSizeBytes {
		return http.StatusBadRequest, []byte("Video too large"), nil
	}

	if err != nil {
		return http.StatusInternalServerError, nil, errors.Wrap(err, "failed to upload video")
	}

	if err := tx.Commit(); err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if err := r.Transcoder.Queue(context.Background(), model); err != nil {
		return http.StatusInternalServerError, nil, err
	}

	return modelsx.ClipFromModel(model).Marshal()
}

func (r *Routes) GetClip(user *models.User, req *http.Request) (int, []byte, error) {
	vars := vars(req)

	clip, err := r.Clips.Find(req.Context(), vars.CID)

	if err == sql.ErrNoRows {
		return http.StatusNotFound, nil, nil
	} else if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	return modelsx.ClipFromModel(clip).Marshal()
}

// GetClipProgress returns the progress of a clip, if it's being processed
// Reports -1 if the processing has not yet begun, or a number between 0 and 100 based on the progress
// Returns 204 if the clip is not being processed, or is done processing
func (r *Routes) GetProgress(user *models.User, req *http.Request) (int, []byte, error) {
	queryparams := query(req)

	clip, err := r.Clips.FindMany(req.Context(), models.ClipWhere.ID.IN(queryparams.CID))

	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if len(clip) == 0 {
		return http.StatusNotFound, nil, nil
	}

	prog := &modelsx.Progress{
		Clips: make(map[modelsx.HashID]int),
	}

	for _, c := range clip {
		if c.Processing {
			progress, ok := r.Transcoder.GetProgress(c.ID)

			if !ok {
				continue
			}

			prog.Clips[modelsx.HashID(c.ID)] = progress
		}
	}

	if len(prog.Clips) == 0 {
		return http.StatusNoContent, nil, nil
	}

	return prog.Marshal()
}

func (r *Routes) GetClips(user *models.User, req *http.Request) (int, []byte, error) {
	clips, err := r.Clips.FindMany(
		req.Context(),
		getPaginationMods(req, models.ClipColumns.CreatedAt, models.TableNames.Clips, models.ClipColumns.ID)...,
	)

	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if len(clips) == 0 {
		return http.StatusNoContent, nil, nil
	}

	return modelsx.ClipFromModelBatch(clips...).Marshal()
}

func (r *Routes) SearchClips(user *models.User, req *http.Request) (int, []byte, error) {
	clips, err := r.Clips.SearchMany(req.Context(), req.URL.Query().Get("query"))

	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if len(clips) == 0 {
		return http.StatusNoContent, nil, nil
	}

	return modelsx.ClipFromModelBatch(clips...).Marshal()
}

func (r *Routes) UpdateClip(user *models.User, req *http.Request) (int, []byte, error) {
	if user == nil {
		return http.StatusUnauthorized, nil, nil
	}

	vars := vars(req)

	clip, err := r.Clips.Find(req.Context(), vars.CID)

	if err == sql.ErrNoRows {
		return http.StatusNotFound, nil, nil
	} else if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if clip.CreatorID != user.ID {
		return http.StatusForbidden, nil, nil
	}

	// Parse the json into a clip
	clipx, err := modelsx.ParseClip(req.Body)

	if err != nil {
		return http.StatusBadRequest, []byte(err.Error()), nil
	}

	model := clipx.ToModel()

	// Update the clip

	if err := r.Clips.Update(req.Context(), model, boil.Whitelist(clipx.GetUpdateWhitelist()...)); err != nil {
		return http.StatusInternalServerError, nil, err
	}

	return modelsx.ClipFromModel(model).Marshal()
}

func (r *Routes) DeleteClip(user *models.User, req *http.Request) (int, []byte, error) {
	if user == nil {
		return http.StatusUnauthorized, nil, nil
	}

	vars := vars(req)

	clip, err := r.Clips.Find(req.Context(), vars.CID)

	if err == sql.ErrNoRows {
		return http.StatusNotFound, nil, nil
	} else if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if clip.CreatorID != user.ID {
		return http.StatusForbidden, nil, nil
	}

	// Delete the clip
	if err := r.Clips.Delete(req.Context(), clip); err != nil {
		return http.StatusInternalServerError, nil, err
	}

	return http.StatusNoContent, nil, nil
}
