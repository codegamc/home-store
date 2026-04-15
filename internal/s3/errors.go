package s3

// S3 error codes
const (
	NoSuchBucket         = "NoSuchBucket"
	NoSuchKey            = "NoSuchKey"
	AccessDenied         = "AccessDenied"
	BucketAlreadyExists  = "BucketAlreadyExists"
	InvalidBucketName    = "InvalidBucketName"
	InvalidRequest       = "InvalidRequest"
	NotImplemented       = "NotImplemented"
	InternalError        = "InternalError"
	ServiceUnavailable   = "ServiceUnavailable"
	SignatureDoesNotMatch = "SignatureDoesNotMatch"
)
