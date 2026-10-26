package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/internal/bootstrap"
	"github.com/master-abror/zago-core/backend/pkg/config"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

const bootstrapUsage = "usage: api bootstrap-admin --email <email> [--name <nama tampilan>] [--password-stdin]"

// bootstrapTimeout membatasi seluruh perintah (koneksi + hash Argon2id + transaksi).
const bootstrapTimeout = 60 * time.Second

// BootstrapAdmin menjalankan `api bootstrap-admin` (docs/16 §2.4, docs/04 §16, E:B-04).
// Urutan: argumen -> konfigurasi (role API: perintah ini hidup di binary api dan memakai
// environment yang sama) -> password dari stdin -> validasi input -> database. Masukan buruk
// selalu ExitConfig SEBELUM menyentuh database; kegagalan runtime ExitFailed. Password tidak
// pernah lewat argumen/environment/log; password acak hanya dicetak bila benar-benar tersimpan.
func BootstrapAdmin(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "email Super Admin")
	name := fs.String("name", "", "nama tampilan (bawaan: bagian lokal email)")
	passwordStdin := fs.Bool("password-stdin", false, "baca password dari satu baris stdin")

	badInput := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(stderr, "bootstrap-admin: "+format+"\n", a...)
		return ExitConfig
	}
	if err := fs.Parse(args); err != nil {
		_, _ = fmt.Fprintf(stderr, "bootstrap-admin: %v\n%s\n", err, bootstrapUsage)
		return ExitConfig
	}
	if fs.NArg() != 0 || strings.TrimSpace(*email) == "" {
		_, _ = fmt.Fprintln(stderr, bootstrapUsage)
		return ExitConfig
	}

	cfg, err := config.LoadFrom(config.RoleAPI, env)
	if err != nil {
		return badInput("%v", err)
	}

	password := ""
	if *passwordStdin {
		if password, err = readPasswordLine(stdin); err != nil {
			return badInput("%v", err)
		}
	}

	hasher, err := security.NewArgon2idHasher(cfg.Argon2Params(), cfg.PasswordHashConcurrency)
	if err != nil {
		return badInput("hasher password: %v", err)
	}
	b, err := bootstrap.New(hasher, cfg.PasswordPolicy())
	if err != nil {
		return badInput("%v", err)
	}
	params := bootstrap.Params{Email: *email, Password: password, DisplayName: *name}
	if _, err = b.Validate(params); err != nil {
		return badInput("%v", err)
	}

	ctx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "bootstrap-admin: gagal terhubung ke database: %s\n", redactSecrets(err.Error(), cfg.DatabaseURL))
		return ExitFailed
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	res, err := b.Run(ctx, conn, params)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "bootstrap-admin: %s\n", redactSecrets(err.Error(), cfg.DatabaseURL))
		return ExitFailed
	}

	if !res.Changed() {
		_, _ = fmt.Fprintf(stdout, "bootstrap-admin: tidak ada perubahan — %s sudah Super Admin platform dengan password; password tidak diubah.\n", res.Email)
		return ExitOK
	}
	_, _ = fmt.Fprintf(stdout, "bootstrap-admin: selesai untuk %s (user %s)\n", res.Email, res.UserID)
	for _, step := range []struct {
		done  bool
		label string
	}{
		{res.RoleCreated, "role Super Admin dibuat"},
		{res.UserCreated, "user dibuat"},
		{res.CredentialCreated, "kredensial password dibuat"},
		{res.AssignmentCreated, "assignment Super Admin (scope platform) dibuat"},
	} {
		if step.done {
			_, _ = fmt.Fprintf(stdout, "  - %s\n", step.label)
		}
	}
	if res.GeneratedPassword != "" {
		_, _ = fmt.Fprintf(stdout, "password: %s\n", res.GeneratedPassword)
		_, _ = fmt.Fprintln(stderr, "bootstrap-admin: simpan password di atas SEKARANG; ia tidak akan ditampilkan lagi.")
	}
	return ExitOK
}

// readPasswordLine membaca SATU baris dari stdin (tanpa CR/LF akhir). Batas bacaan sedikit di
// atas MaxPasswordBytes: baris lebih panjang terpotong lalu ditolak kebijakan, bukan dibaca habis.
func readPasswordLine(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("stdin tidak tersedia untuk --password-stdin")
	}
	br := bufio.NewReader(io.LimitReader(r, security.MaxPasswordBytes+3))
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("gagal membaca password dari stdin: %w", err)
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", errors.New("password dari stdin kosong")
	}
	return line, nil
}

// redactSecrets membuang password dan URL lengkap dari pesan galat (CLAUDE.md: rahasia tak
// pernah di-log). Driver kadang menyertakan DSN pada galat koneksi.
func redactSecrets(msg, rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "***")
		}
	}
	if rawURL != "" {
		msg = strings.ReplaceAll(msg, rawURL, "<url>")
	}
	return msg
}
