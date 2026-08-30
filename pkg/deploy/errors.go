package deploy

import "errors"

var (
	// ErrInvalidNudgeToken is returned when the webhook token is unknown, expired, or inactive.
	ErrInvalidNudgeToken = errors.New("pikpik: invalid or inactive redeploy nudge token")

	// ErrRateLimitExceeded is returned when the webhook request rate limit is exceeded.
	ErrRateLimitExceeded = errors.New("pikpik: rate limit exceeded for webhook endpoint")

	// ErrPayloadTooLarge is returned when the webhook payload exceeds 64KB ceiling.
	ErrPayloadTooLarge = errors.New("pikpik: webhook payload exceeds 64KB ceiling")

	// ErrInvalidImageReference is returned when the OCI image name does not match specification.
	ErrInvalidImageReference = errors.New("pikpik: invalid OCI image reference format")

	// ErrUnauthorizedRegistry is returned when the image registry is not in the allowed list.
	ErrUnauthorizedRegistry = errors.New("pikpik: image registry domain is not in allowed list")
)
