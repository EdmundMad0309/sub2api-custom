package service

// ExcelBPS403DisabledAtKey records when an upstream HTTP 403 automatically
// turned Excel BPS off (RFC 3339, UTC). Only DisableExcelBPSOn403 writes it; the
// admin account list shows it as a suspected Excel ban.
const ExcelBPS403DisabledAtKey = "openai_excel_bps_403_disabled_at"

// MergeExcelBPS403Marker keeps the persisted marker across account edits and
// ignores a value supplied by the edit. Turning Excel BPS back on acknowledges
// the 403 and clears it. The repository applies this under the row lock.
func MergeExcelBPS403Marker(extra, current map[string]any) map[string]any {
	delete(extra, ExcelBPS403DisabledAtKey)
	if enabled, _ := extra["openai_excel_bps"].(bool); enabled {
		return extra
	}
	value, ok := current[ExcelBPS403DisabledAtKey]
	if !ok {
		return extra
	}
	if extra == nil {
		extra = make(map[string]any, 1)
	}
	extra[ExcelBPS403DisabledAtKey] = value
	return extra
}
