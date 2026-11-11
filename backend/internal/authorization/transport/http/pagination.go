package authzhttp

import (
	"net/url"
	"strconv"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Paginasi offset untuk daftar kecil yang dikelola admin (docs/08 §4: GET /roles?page=1&per_page=25,
// meta.pagination {page, per_page, total}). Daftar yang tumbuh tanpa batas memakai cursor
// (httpx.ParsePage), bukan ini.

// maxPageNumber membatasi page supaya offset (page-1)*per_page tidak dapat meluap dan tidak ada
// permintaan offset raksasa ke PostgreSQL.
const maxPageNumber = 10000

// parsePaging membaca page dan per_page dari query. Nilai yang hilang memakai bawaan (page 1,
// per_page 25); per_page di atas batas dipangkas ke domain.MaxPageSize (bukan ditolak, seperti
// limit cursor); nilai bukan angka, nol, atau negatif menambah satu galat ke f.
func parsePaging(q url.Values, f *httpx.Fields) (page, perPage int) {
	page, perPage = 1, domain.DefaultPageSize

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageNumber {
			f.Add("page", "invalid", "page must be an integer between 1 and 10000")
		} else {
			page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil || n < 1:
			f.Add("per_page", "invalid", "per_page must be a positive integer")
		case n > domain.MaxPageSize:
			perPage = domain.MaxPageSize
		default:
			perPage = n
		}
	}
	return page, perPage
}

// windowFor mengubah nomor halaman menjadi jendela offset.
func windowFor(page, perPage int) domain.Window {
	return domain.Window{Offset: (page - 1) * perPage, Limit: perPage}
}

// paginationMeta membentuk meta.pagination untuk respons daftar.
func paginationMeta(page, perPage, total int) httpx.Meta {
	return httpx.Meta{"pagination": map[string]int{"page": page, "per_page": perPage, "total": total}}
}
