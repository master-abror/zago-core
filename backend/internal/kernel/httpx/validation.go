package httpx

// FieldError adalah satu galat validasi pada satu field (docs/08 §3.2: details machine-readable).
// Code adalah kode stabil untuk katalog terjemahan frontend (mis. "required", "too_long").
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// Fields mengumpulkan galat validasi per field agar semuanya dikembalikan sekaligus.
type Fields struct {
	errs []FieldError
}

// Add menambahkan satu galat.
func (f *Fields) Add(field, code, message string) {
	f.errs = append(f.errs, FieldError{Field: field, Code: code, Message: message})
}

// Len mengembalikan jumlah galat terkumpul.
func (f *Fields) Len() int { return len(f.errs) }

// Err mengembalikan nil bila tidak ada galat, selain itu validation_failed dengan
// details {"fields":[{field,code,message}...]}.
func (f *Fields) Err() error {
	if len(f.errs) == 0 {
		return nil
	}
	return Validation(f.errs...)
}

// Validation membuat validation_failed dari galat field yang diberikan.
func Validation(errs ...FieldError) *Error {
	cp := make([]FieldError, len(errs))
	copy(cp, errs)
	return ValidationFailed.New().WithDetails(map[string]any{"fields": cp})
}
