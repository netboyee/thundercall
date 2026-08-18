package twilio

import (
	"errors"
	"net"

	twilioclient "github.com/twilio/twilio-go/client"
)

func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var restV1 *twilioclient.RestErrorV1
	if errors.As(err, &restV1) {
		return restV1.HttpStatusCode == 429 || restV1.HttpStatusCode >= 500
	}

	var restErr *twilioclient.TwilioRestError
	if errors.As(err, &restErr) {
		return restErr.Status == 429 || restErr.Status >= 500 || restErr.Code == 20429
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	return false
}
