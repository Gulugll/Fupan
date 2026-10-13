package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

// InsertChunks 批量插入切片。硬约束：content 只 INSERT，任何代码路径都不提供 UPDATE。
func (s *store) InsertChunks(ctx context.Context, chunks []domain.Chunk) error {
	for i := range chunks {
		c := &chunks[i]
		_, err := s.q.ExecContext(ctx,
			`INSERT INTO chunks(id, doc_id, parent_id, kind, title_path, seq, content, char_start, char_end, author, version, created_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.DocID, nullable(c.ParentID), string(c.Kind), c.TitlePath, c.Seq,
			c.Content, c.CharStart, c.CharEnd, string(c.Author), c.Version, c.CreatedAt.Format(time.RFC3339))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *store) GetChunk(ctx context.Context, id string) (*domain.Chunk, error) {
	var c domain.Chunk
	var kind, author, ts string
	var parent sql.NullString
	err := s.q.QueryRowContext(ctx,
		`SELECT id,doc_id,parent_id,kind,title_path,seq,content,char_start,char_end,author,version,created_at
		 FROM chunks WHERE id=?`, id).
		Scan(&c.ID, &c.DocID, &parent, &kind, &c.TitlePath, &c.Seq, &c.Content, &c.CharStart, &c.CharEnd, &author, &c.Version, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.ParentID = parent.String
	c.Kind = domain.ChunkKind(kind)
	c.Author = domain.ChunkAuthor(author)
	c.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &c, nil
}

func (s *store) ListChunksByDoc(ctx context.Context, docID string) ([]domain.Chunk, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id,doc_id,parent_id,kind,title_path,seq,content,char_start,char_end,author,version,created_at
		 FROM chunks WHERE doc_id=? ORDER BY seq`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Chunk
	for rows.Next() {
		var c domain.Chunk
		var kind, author, ts string
		var parent sql.NullString
		if err := rows.Scan(&c.ID, &c.DocID, &parent, &kind, &c.TitlePath, &c.Seq, &c.Content, &c.CharStart, &c.CharEnd, &author, &c.Version, &ts); err != nil {
			return nil, err
		}
		c.ParentID = parent.String
		c.Kind = domain.ChunkKind(kind)
		c.Author = domain.ChunkAuthor(author)
		c.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, c)
	}
	return out, rows.Err()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
