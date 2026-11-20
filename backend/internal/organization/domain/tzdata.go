package domain

// Zona waktu ikut tertanam di biner supaya validasi default_timezone tidak bergantung pada
// basis data zona waktu image runtime.
import _ "time/tzdata"
