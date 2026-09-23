package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/dto"
	"gorm.io/gorm"
)

var ErrVersionConflict = errors.New("record was changed by another request")

type txContextKey struct{}

// WithTx returns a context bound to an existing database transaction. All
// repositories resolve the connection from the context, so writes made through
// different repositories inside the same tx share one transaction.
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// conn returns the transaction carried by ctx when present, otherwise the pool.
func (s *Store[T]) conn(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx)
	}
	return s.pool.WithContext(ctx)
}

// Conn resolves the tx-aware connection for a pool inside repository code that
// does not go through Store.
func Conn(ctx context.Context, pool *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx)
	}
	return pool.WithContext(ctx)
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// Store centralizes consistent paging and optimistic-lock semantics while
// concrete repository files retain an explicit boundary for each aggregate.
type Store[T any] struct {
	pool *gorm.DB
}

func NewStore[T any](db *gorm.DB) *Store[T] { return &Store[T]{pool: db} }

func (s *Store[T]) List(ctx context.Context, query dto.PageQuery) (Page[T], error) {
	page, pageSize := normalizePage(query.Page, query.PageSize)
	db := s.conn(ctx).Model(new(T))
	if search := strings.TrimSpace(strings.ToLower(query.Search)); search != "" {
		wildcard := "%" + search + "%"
		db = db.Where("LOWER(code) LIKE ? OR LOWER(name) LIKE ?", wildcard, wildcard)
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[T]{}, err
	}
	items := make([]T, 0)
	err := db.Order("updated_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return Page[T]{Items: items, Total: total, Page: page, PageSize: pageSize}, err
}

func (s *Store[T]) Get(ctx context.Context, id uint) (T, error) {
	var item T
	err := s.conn(ctx).First(&item, id).Error
	return item, err
}

func (s *Store[T]) Create(ctx context.Context, item *T) error {
	return s.conn(ctx).Create(item).Error
}

func (s *Store[T]) Update(ctx context.Context, id, expectedVersion uint, item *T) error {
	result := s.conn(ctx).Model(new(T)).
		Where("id = ? AND version = ?", id, expectedVersion).
		Select("*").Omit("id", "code", "created_at", "deleted_at").Updates(item)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (s *Store[T]) Delete(ctx context.Context, id uint) error {
	result := s.conn(ctx).Delete(new(T), id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *Store[T]) CountByStatus(ctx context.Context) (map[string]int64, error) {
	rows, err := s.conn(ctx).Model(new(T)).
		Select("status, COUNT(*) AS total").Group("status").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int64)
	for rows.Next() {
		var status string
		var total int64
		if err := rows.Scan(&status, &total); err != nil {
			return nil, err
		}
		counts[status] = total
	}
	return counts, rows.Err()
}

// ListByCodes loads records by their human-facing codes, preserving neither
// order nor duplicates. Concurrency safety during multi-row writes comes from
// the conditional version+status UPDATEs rather than row locks, so this also
// works on databases without SELECT ... FOR UPDATE (e.g. SQLite).
func (s *Store[T]) ListByCodes(ctx context.Context, codes []string) ([]T, error) {
	items := make([]T, 0, len(codes))
	if len(codes) == 0 {
		return items, nil
	}
	err := s.conn(ctx).Model(new(T)).Where("code IN ?", codes).Find(&items).Error
	return items, err
}

// ListByFacilityAndStatus loads records belonging to any of the given
// facilities and currently in status. All operational aggregates in this
// project carry the indexed facility column.
func (s *Store[T]) ListByFacilityAndStatus(ctx context.Context, facilities []string, status string) ([]T, error) {
	items := make([]T, 0)
	if len(facilities) == 0 {
		return items, nil
	}
	err := s.conn(ctx).Model(new(T)).
		Where("facility IN ? AND status = ?", facilities, status).
		Find(&items).Error
	return items, err
}

// AdvanceStatus moves a record from expectedStatus to status with optimistic
// locking, bumping the version and updated_at. No row is touched unless both
// the version and the current status still match.
func (s *Store[T]) AdvanceStatus(ctx context.Context, id, expectedVersion uint, expectedStatus, status string) error {
	result := s.conn(ctx).Model(new(T)).
		Where("id = ? AND version = ? AND status = ?", id, expectedVersion, expectedStatus).
		Updates(map[string]any{"status": status, "version": expectedVersion + 1, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

// AdvanceStatusByID moves a record to status without a version precondition,
// for cascades whose ownership belongs to another aggregate. It still bumps
// version and updated_at and reports ErrVersionConflict if the row vanished.
func (s *Store[T]) AdvanceStatusByID(ctx context.Context, id uint, expectedStatus, status string) error {
	result := s.conn(ctx).Model(new(T)).
		Where("id = ? AND status = ?", id, expectedStatus).
		Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
