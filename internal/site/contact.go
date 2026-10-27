package site

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ContactMessage struct {
	ID         int64
	Name       string
	Phone      string
	Email      string
	Company    string
	Subject    string
	Message    string
	SourcePage string
	IP         string
	UserAgent  string
	Status     string
	AdminNote  string
	CreatedAt  int64
}

var ErrInvalidStatus = errors.New("site: invalid message status")

func (s *Store) CreateContactMessage(ctx context.Context, m *ContactMessage) (*ContactMessage, error) {
	if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Message) == "" {
		return nil, errors.New("site: name and message are required")
	}
	if m.Status == "" {
		m.Status = "new"
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO contact_messages(name, phone, email, company, subject, message, source_page, ip, user_agent, status, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Phone, m.Email, m.Company, m.Subject, m.Message, m.SourcePage, m.IP, m.UserAgent, m.Status, nowUnix())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.ContactMessageByID(ctx, id)
}

func (s *Store) ContactMessageByID(ctx context.Context, id int64) (*ContactMessage, error) {
	var m ContactMessage
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(phone,''), COALESCE(email,''), COALESCE(company,''), COALESCE(subject,''),
		       message, COALESCE(source_page,''), COALESCE(ip,''), COALESCE(user_agent,''), status,
		       COALESCE(admin_note,''), created_at
		FROM contact_messages WHERE id = ?`, id).
		Scan(&m.ID, &m.Name, &m.Phone, &m.Email, &m.Company, &m.Subject, &m.Message,
			&m.SourcePage, &m.IP, &m.UserAgent, &m.Status, &m.AdminNote, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListContactMessages(ctx context.Context, status string, limit int) ([]ContactMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT id, name, COALESCE(phone,''), COALESCE(email,''), COALESCE(company,''), COALESCE(subject,''),
		message, COALESCE(source_page,''), COALESCE(ip,''), COALESCE(user_agent,''), status,
		COALESCE(admin_note,''), created_at FROM contact_messages`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContactMessage
	for rows.Next() {
		var m ContactMessage
		if err := rows.Scan(&m.ID, &m.Name, &m.Phone, &m.Email, &m.Company, &m.Subject, &m.Message,
			&m.SourcePage, &m.IP, &m.UserAgent, &m.Status, &m.AdminNote, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateContactMessage(ctx context.Context, id int64, status, note string) error {
	switch status {
	case "new", "read", "replied", "spam":
	default:
		return ErrInvalidStatus
	}
	res, err := s.db.ExecContext(ctx, `UPDATE contact_messages SET status = ?, admin_note = ? WHERE id = ?`, status, note, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CountNewContactMessages(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contact_messages WHERE status = 'new'`).Scan(&n)
	return n, err
}

func nowUnix() int64 {
	return time.Now().UTC().Unix()
}
