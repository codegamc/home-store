package s3

import "encoding/xml"

// ListAllMyBucketsResult is the response to ListBuckets.
type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListAllMyBucketsResult"`
	Owner   Owner    `xml:"Owner"`
	Buckets []Bucket `xml:"Buckets>Bucket"`
}

// Owner represents the owner of buckets/objects.
type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

// Bucket represents a bucket in the list.
type Bucket struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// ListObjectsV2Result is the response to ListObjectsV2.
type ListObjectsV2Result struct {
	XMLName           xml.Name       `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name              string         `xml:"Name"`
	Prefix            string         `xml:"Prefix"`
	MaxKeys           int            `xml:"MaxKeys"`
	IsTruncated       bool           `xml:"IsTruncated"`
	Contents          []Object       `xml:"Contents"`
	CommonPrefixes    []CommonPrefix `xml:"CommonPrefixes"`
	ContinuationToken string         `xml:"ContinuationToken"`
	NextToken         string         `xml:"NextContinuationToken"`
	KeyCount          int            `xml:"KeyCount"`
}

// ListObjectsResult is the response to the ListObjects API.
type ListObjectsResult struct {
	XMLName        xml.Name       `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name           string         `xml:"Name"`
	Prefix         string         `xml:"Prefix"`
	Marker         string         `xml:"Marker"`
	NextMarker     string         `xml:"NextMarker,omitempty"`
	Delimiter      string         `xml:"Delimiter,omitempty"`
	MaxKeys        int            `xml:"MaxKeys"`
	IsTruncated    bool           `xml:"IsTruncated"`
	Contents       []Object       `xml:"Contents"`
	CommonPrefixes []CommonPrefix `xml:"CommonPrefixes"`
}

// CommonPrefix represents a collapsed prefix in an object listing.
type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// DeleteObjectsRequest is the S3 multi-object delete request body.
type DeleteObjectsRequest struct {
	Objects []ObjectIdentifier `xml:"Object"`
	Quiet   bool               `xml:"Quiet"`
}

type ObjectIdentifier struct {
	Key string `xml:"Key"`
}

// DeleteObjectsResult is the S3 multi-object delete response body.
type DeleteObjectsResult struct {
	XMLName xml.Name        `xml:"DeleteResult"`
	Deleted []DeletedObject `xml:"Deleted"`
}

type DeletedObject struct {
	Key string `xml:"Key"`
}

// InitiateMultipartUploadResult is returned when a multipart upload starts.
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadID string   `xml:"UploadId"`
}

// CompleteMultipartUploadRequest is the S3 completion request body.
type CompleteMultipartUploadRequest struct {
	Parts []CompletedPart `xml:"Part"`
}

type CompletedPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

// CompleteMultipartUploadResult is returned after an upload is finalized.
type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// ListPartsResult is the S3 response for a multipart upload's parts.
type ListPartsResult struct {
	XMLName  xml.Name        `xml:"ListPartsResult"`
	Bucket   string          `xml:"Bucket"`
	Key      string          `xml:"Key"`
	UploadID string          `xml:"UploadId"`
	Parts    []MultipartPart `xml:"Part"`
}

type MultipartPart struct {
	PartNumber   int    `xml:"PartNumber"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

// ListMultipartUploadsResult is the S3 response for in-progress uploads.
type ListMultipartUploadsResult struct {
	XMLName xml.Name          `xml:"ListMultipartUploadsResult"`
	Bucket  string            `xml:"Bucket"`
	Uploads []MultipartUpload `xml:"Upload"`
}

type MultipartUpload struct {
	Key       string `xml:"Key"`
	UploadID  string `xml:"UploadId"`
	Initiated string `xml:"Initiated"`
}

// CopyPartResult is returned by UploadPartCopy.
type CopyPartResult struct {
	XMLName      xml.Name `xml:"CopyPartResult"`
	ETag         string   `xml:"ETag"`
	LastModified string   `xml:"LastModified"`
}

// GetObjectAttributesResult is the response to GetObjectAttributes.
type GetObjectAttributesResult struct {
	XMLName      xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ GetObjectAttributesOutput"`
	ETag         string   `xml:"ETag,omitempty"`
	ObjectSize   int64    `xml:"ObjectSize,omitempty"`
	StorageClass string   `xml:"StorageClass,omitempty"`
}

// Object represents an object in a bucket listing.
type Object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// CopyObjectResult is the response to CopyObject.
type CopyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	ETag         string   `xml:"ETag"`
	LastModified string   `xml:"LastModified"`
}

// ErrorResponse is an S3 error response.
type ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId"`
}
