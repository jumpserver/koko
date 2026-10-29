package cache

import (
	"net/http"

	"github.com/jumpserver-dev/sdk-go/service"
)

// CanCacheConnectToken keeps client login expiry checks on Core. A cached token
// would let a new SFTP connection bypass those checks after logout or timeout.
func CanCacheConnectToken(jmsService *service.JMService, tokenID string) bool {
	var source *struct {
		ClientLoginSession *string `json:"client_login_session"`
	}
	response, err := jmsService.Call("GET", service.SuperConnectTokenInfoURL+tokenID+"/", nil, &source)
	// Older Core versions omit this field. Preserve their cache behavior, but
	// skip caching when the source cannot be verified; the current connection
	// has already been authenticated and does not depend on this cache entry.
	return err == nil && response != nil && response.StatusCode == http.StatusOK &&
		source != nil && source.ClientLoginSession == nil
}
