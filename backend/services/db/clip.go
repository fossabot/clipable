package db

import (
	"context"
	"database/sql"
	"io"
	"webserver/models"
	"webserver/modelsx"
	"webserver/services"

	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

type clips struct {
	db *sql.DB
	os services.ObjectStore
}

// NewClips Comment for linter
func NewClips(db *sql.DB, os services.ObjectStore) services.Clips {
	return &clips{db, os}
}

func (c *clips) Find(ctx context.Context, cid int64) (*models.Clip, error) {
	return models.Clips(
		qm.Load(models.ClipRels.Creator),
		models.ClipWhere.ID.EQ(cid),
	).One(ctx, c.db)
}

func (c *clips) FindMany(ctx context.Context, mods ...qm.QueryMod) (models.ClipSlice, error) {
	return models.Clips(modelsx.NewBuilder().
		Add(mods...).
		Add(qm.Load(models.ClipRels.Creator))...,
	).All(ctx, c.db)
}

func (c *clips) Exists(ctx context.Context, cid int64) (bool, error) {
	return models.ClipExists(ctx, c.db, cid)
}

func (c *clips) Delete(ctx context.Context, clip *models.Clip) error {
	_, err := clip.Delete(ctx, c.db)
	return err
}

func (c *clips) SearchMany(ctx context.Context, query string) (models.ClipSlice, error) {
	return models.Clips(
		qm.Select("*"),
		qm.Where(`f_concat_ws(' ', title, "description") ILIKE ?`, "%"+query+"%"),
		qm.OrderBy(`f_concat_ws(' ', title, "description") <-> ?`, "%"+query+"%"),
		qm.Limit(10),
		qm.Load(models.ClipRels.Creator),
	).All(ctx, c.db)
}

func (c *clips) Update(ctx context.Context, clip *models.Clip, columns boil.Columns) error {
	_, err := clip.Update(ctx, c.db, columns)
	return err
}

func (c *clips) Create(ctx context.Context, clip *models.Clip, creator *models.User, columns boil.Columns) (services.ClipTx, error) {
	tx, err := c.db.BeginTx(ctx, nil)

	if err != nil {
		return nil, err
	}

	columns.Cols = append(columns.Cols, models.ClipColumns.CreatorID)
	clip.CreatorID = creator.ID

	if err := clip.Insert(ctx, tx, columns); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := clip.SetCreator(ctx, tx, false, creator); err != nil {
		tx.Rollback()
		return nil, err
	}

	return &clipTx{tx, clip, c.os, false}, nil
}

type clipTx struct {
	tx   *sql.Tx
	clip *models.Clip
	os   services.ObjectStore

	done bool
}

func (c *clipTx) UploadVideo(ctx context.Context, r io.Reader) (int64, error) {
	return c.os.PutObject(ctx, c.clip.ID, "raw", r)
}

func (c *clipTx) Commit() error {
	err := c.tx.Commit()

	c.done = err == nil

	return err
}

func (c *clipTx) Rollback() error {
	if !c.done {
		if err := c.os.DeleteObject(context.Background(), c.clip.ID, "raw"); err != nil {
			return err
		}
	}
	return c.tx.Rollback()
}
