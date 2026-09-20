package state

import "strings"

// ChatModelPreferenceKey deliberately excludes workspace and actor identities.
// Gateway and provider keep unrelated bots and model catalogs isolated.
func ChatModelPreferenceKey(gatewayID, chatID, providerID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	return strings.Join([]string{"codex", NormalizeCodexProviderID(providerID), strings.TrimSpace(gatewayID), chatID}, "\x00")
}
