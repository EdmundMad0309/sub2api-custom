package service

import "strings"

// maskUpstreamModelName returns the (from, to) pair used to hide the
// upstream-reported model name from clients.
func maskUpstreamModelName(account *Account, originalModel, observedModel string) (string, string, bool) {
	if account == nil || !account.MasksUpstreamResponseModel() {
		return "", "", false
	}
	from := strings.TrimSpace(observedModel)
	to := strings.TrimSpace(originalModel)
	if from == "" || to == "" || from == to {
		return "", "", false
	}
	return from, to, true
}
