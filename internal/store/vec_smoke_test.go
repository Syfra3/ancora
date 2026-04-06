package store

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"math"
	"testing"

	_ "modernc.org/sqlite"
)

// serializeFloat32 encodes a float32 slice as little-endian bytes.
// This is the vector storage format used in the ancora store —
// no external C library required; pure Go via modernc.org/sqlite.
func serializeFloat32(v []float32) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, v)
	return buf.Bytes()
}

// deserializeFloat32 decodes a little-endian byte slice back to float32.
func deserializeFloat32(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	_ = binary.Read(bytes.NewReader(b), binary.LittleEndian, &v)
	return v
}

// cosineSimilarity computes the cosine similarity between two equal-length vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

// TestVecSmokeFloat32Blob verifies that we can store and retrieve float32
// vectors as BLOBs in SQLite using modernc.org/sqlite (no CGO required).
func TestVecSmokeFloat32Blob(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	// Create a simple table with a vector column stored as BLOB.
	if _, err := db.Exec(`
		CREATE TABLE vecs (
			id    INTEGER PRIMARY KEY,
			label TEXT NOT NULL,
			vec   BLOB
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert two test vectors.
	vec1 := []float32{1.0, 0.0, 0.0}
	vec2 := []float32{0.0, 1.0, 0.0}

	if _, err := db.Exec(`INSERT INTO vecs (label, vec) VALUES (?, ?)`, "x-axis", serializeFloat32(vec1)); err != nil {
		t.Fatalf("insert vec1: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO vecs (label, vec) VALUES (?, ?)`, "y-axis", serializeFloat32(vec2)); err != nil {
		t.Fatalf("insert vec2: %v", err)
	}

	// Query both vectors back and verify.
	rows, err := db.Query(`SELECT label, vec FROM vecs ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type row struct {
		label string
		vec   []float32
	}
	var results []row

	for rows.Next() {
		var label string
		var blob []byte
		if err := rows.Scan(&label, &blob); err != nil {
			t.Fatalf("scan: %v", err)
		}
		results = append(results, row{label, deserializeFloat32(blob)})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// Verify round-trip.
	if results[0].label != "x-axis" || results[0].vec[0] != 1.0 || results[0].vec[1] != 0.0 {
		t.Errorf("vec1 round-trip failed: %+v", results[0])
	}
	if results[1].label != "y-axis" || results[1].vec[0] != 0.0 || results[1].vec[1] != 1.0 {
		t.Errorf("vec2 round-trip failed: %+v", results[1])
	}

	// Verify cosine similarity: x-axis vs itself = 1.0, x-axis vs y-axis = 0.0.
	sim11 := cosineSimilarity(vec1, vec1)
	if math.Abs(float64(sim11)-1.0) > 1e-6 {
		t.Errorf("cosineSim(vec1, vec1) = %f, want 1.0", sim11)
	}

	sim12 := cosineSimilarity(vec1, vec2)
	if math.Abs(float64(sim12)) > 1e-6 {
		t.Errorf("cosineSim(vec1, vec2) = %f, want 0.0", sim12)
	}

	t.Logf("Vector storage approach: float32 BLOB via modernc.org/sqlite (no CGO)")
	t.Logf("cosineSim(x, x)=%.3f  cosineSim(x, y)=%.3f", sim11, sim12)
}
