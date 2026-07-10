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

// CreateBucketConfiguration is the XML body for CreateBucket.
type CreateBucketConfiguration struct {
	XMLName            xml.Name `xml:"CreateBucketConfiguration"`
	LocationConstraint string   `xml:"LocationConstraint"`
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

// ListObjectsResult is the legacy marker-based listing response.
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

type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
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

type DeleteRequest struct {
	XMLName xml.Name         `xml:"Delete"`
	Objects []ObjectToDelete `xml:"Object"`
	Quiet   bool             `xml:"Quiet"`
}

type ObjectToDelete struct {
	Key string `xml:"Key"`
}

type DeleteResult struct {
	XMLName xml.Name        `xml:"http://s3.amazonaws.com/doc/2006-03-01/ DeleteResult"`
	Deleted []DeletedObject `xml:"Deleted,omitempty"`
	Errors  []DeleteError   `xml:"Error,omitempty"`
}

type DeletedObject struct {
	Key string `xml:"Key"`
}
type DeleteError struct {
	Key     string `xml:"Key"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadID string   `xml:"UploadId"`
}

type CompleteMultipartUploadRequest struct {
	XMLName xml.Name        `xml:"CompleteMultipartUpload"`
	Parts   []CompletedPart `xml:"Part"`
}

type CompletedPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

type CopyPartResult struct {
	XMLName      xml.Name `xml:"CopyPartResult"`
	LastModified string   `xml:"LastModified"`
	ETag         string   `xml:"ETag"`
}

type ListPartsResult struct {
	XMLName     xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListPartsResult"`
	Bucket      string   `xml:"Bucket"`
	Key         string   `xml:"Key"`
	UploadID    string   `xml:"UploadId"`
	Parts       []Part   `xml:"Part"`
	IsTruncated bool     `xml:"IsTruncated"`
}

type Part struct {
	PartNumber   int    `xml:"PartNumber"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

type ListMultipartUploadsResult struct {
	XMLName     xml.Name          `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListMultipartUploadsResult"`
	Bucket      string            `xml:"Bucket"`
	Prefix      string            `xml:"Prefix"`
	Uploads     []MultipartUpload `xml:"Upload"`
	IsTruncated bool              `xml:"IsTruncated"`
}

type MultipartUpload struct {
	Key          string `xml:"Key"`
	UploadID     string `xml:"UploadId"`
	Initiator    Owner  `xml:"Initiator"`
	Owner        Owner  `xml:"Owner"`
	StorageClass string `xml:"StorageClass"`
	Initiated    string `xml:"Initiated"`
}

// ErrorResponse is an S3 error response.
type ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId"`
}
