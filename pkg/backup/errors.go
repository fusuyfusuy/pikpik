package backup

import "errors"

var (
	// ErrStreamingPipeFailed is returned when the streaming io pipe encounters an unrecoverable error.
	ErrStreamingPipeFailed = errors.New("pikpik: streaming io pipe encountered an unrecoverable IO error")

	// ErrContainerExecFailed is returned when database dump or restore process exits with non-zero code.
	ErrContainerExecFailed = errors.New("pikpik: database dump process exited with non-zero exit code")

	// ErrRestoreStdinClosed is returned when database restore process terminates stdin unexpectedly.
	ErrRestoreStdinClosed = errors.New("pikpik: database restore process terminated stdin unexpectedly")

	// ErrBackupMemoryCeilingHit is returned when memory buffer exceeds the 32MB safety ceiling.
	ErrBackupMemoryCeilingHit = errors.New("pikpik: memory buffer exceeded 32MB safety ceiling")

	// ErrS3UploadAborted is returned when S3 multipart upload is aborted due to stream failure.
	ErrS3UploadAborted = errors.New("pikpik: s3 multipart upload aborted due to stream failure")

	// ErrS3ObjectNotFound is returned when the requested S3 backup key cannot be located.
	ErrS3ObjectNotFound = errors.New("pikpik: requested s3 backup key not found")

	// ErrS3InvalidCredentials is returned when SigV4 authentication fails with S3 provider.
	ErrS3InvalidCredentials = errors.New("pikpik: sigv4 authentication failed with s3 provider")

	// ErrS3EndpointUnreachable is returned when the remote S3 endpoint cannot be reached.
	ErrS3EndpointUnreachable = errors.New("pikpik: remote s3 endpoint could not be reached")
)
