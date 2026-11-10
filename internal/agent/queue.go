package agent

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

// Queue guarda os lotes ainda não enviados (internet caiu, servidor fora) num SQLite local,
// com cada lote criptografado (AES-256-GCM). A chave fica protegida pelo Windows (DPAPI).
type Queue struct {
	db  *sql.DB
	gcm cipher.AEAD
}

const maxQueued = 20_000 // ~2 semanas de lotes de 1 min; acima disso descarta os mais antigos

func OpenQueue(path string, key []byte) (*Queue, error) {
	if len(key) != 32 {
		return nil, errors.New("chave da fila deve ter 32 bytes")
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`create table if not exists batches (id integer primary key, created_at integer not null, data blob not null)`); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Queue{db: db, gcm: gcm}, nil
}

func (q *Queue) Close() error { return q.db.Close() }

func (q *Queue) Push(b *Batch) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	nonce := make([]byte, q.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	if _, err := q.db.Exec(`insert into batches (created_at, data) values (?, ?)`, time.Now().Unix(), q.gcm.Seal(nonce, nonce, raw, nil)); err != nil {
		return err
	}
	_, err = q.db.Exec(`delete from batches where id <= (select max(id) from batches) - ?`, maxQueued)
	return err
}

// Peek devolve até n lotes, do mais antigo para o mais novo. Lotes ilegíveis são descartados.
func (q *Queue) Peek(n int) (ids []int64, batches []*Batch, err error) {
	rows, err := q.db.Query(`select id, data from batches order by id limit ?`, n)
	if err != nil {
		return nil, nil, err
	}
	var bad []int64
	for rows.Next() {
		var id int64
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			rows.Close()
			return nil, nil, err
		}
		ns := q.gcm.NonceSize()
		var b Batch
		if len(data) <= ns {
			bad = append(bad, id)
			continue
		}
		plain, err := q.gcm.Open(nil, data[:ns], data[ns:], nil)
		if err != nil || json.Unmarshal(plain, &b) != nil {
			bad = append(bad, id)
			continue
		}
		ids = append(ids, id)
		batches = append(batches, &b)
	}
	err = rows.Err()
	rows.Close() // a fila usa uma conexão só: libera antes de apagar
	for _, id := range bad {
		_, _ = q.db.Exec(`delete from batches where id = ?`, id)
	}
	return ids, batches, err
}

func (q *Queue) Ack(id int64) error {
	_, err := q.db.Exec(`delete from batches where id = ?`, id)
	return err
}

func (q *Queue) Len() int {
	var n int
	_ = q.db.QueryRow(`select count(*) from batches`).Scan(&n)
	return n
}

// Flush envia a fila em ordem; para no primeiro erro (sem internet) e tenta de novo no próximo ciclo.
func (q *Queue) Flush(c *Client, onConfig func(*ServerConfig)) error {
	for {
		ids, batches, err := q.Peek(20)
		if err != nil || len(ids) == 0 {
			return err
		}
		for i, b := range batches {
			cfg, err := c.Sync(b)
			if err != nil {
				return err
			}
			if onConfig != nil {
				onConfig(cfg)
			}
			if err := q.Ack(ids[i]); err != nil {
				return err
			}
		}
	}
}
