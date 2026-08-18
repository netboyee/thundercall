package accounts

import (
	"context"
	"database/sql"
	"errors"

	"thundercall-go/internal/models"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, account *models.Account) error {
	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO accounts (name, active) VALUES (?, ?)`,
		account.Name,
		account.Active,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	account.ID = id
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id int64) (*models.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, active, created_at, updated_at
		FROM accounts
		WHERE id = ?`,
		id,
	)
	return scanAccount(row)
}

func (r *Repository) List(ctx context.Context) ([]models.Account, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, active, created_at, updated_at
		FROM accounts
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *account)
	}

	return accounts, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(s scanner) (*models.Account, error) {
	var account models.Account

	err := s.Scan(
		&account.ID,
		&account.Name,
		&account.Active,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &account, nil
}
