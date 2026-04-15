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
	XMLName           xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name              string   `xml:"Name"`
	Prefix            string   `xml:"Prefix"`
	MaxKeys           int      `xml:"MaxKeys"`
	IsTruncated       bool     `xml:"IsTruncated"`
	Contents          []Object `xml:"Contents"`
	CommonPrefixes    []string `xml:"CommonPrefixes>Prefix"`
	ContinuationToken string   `xml:"ContinuationToken"`
	NextToken         string   `xml:"NextContinuationToken"`
	KeyCount          int      `xml:"KeyCount"`
}

// Object represents an object in a bucket listing.
type Object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// ErrorResponse is an S3 error response.
type ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId"`
}
