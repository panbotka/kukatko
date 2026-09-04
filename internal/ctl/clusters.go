package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ClusterFace is one face shown for a cluster: enough to find it again (its photo
// and per-photo index) and to crop it out of a thumbnail (the normalised box).
type ClusterFace struct {
	PhotoUID  string     `json:"photo_uid"`
	FaceIndex int        `json:"face_index"`
	BBox      [4]float64 `json:"bbox"`
	DetScore  float64    `json:"det_score"`
}

// Cluster is one group of unassigned faces the auto-clustering found: how many
// faces it holds, a representative and a few examples, and — when a named subject
// is close enough — who the server thinks it is.
type Cluster struct {
	UID            string          `json:"uid"`
	Size           int             `json:"size"`
	Representative ClusterFace     `json:"representative"`
	Examples       []ClusterFace   `json:"examples"`
	Suggestion     *FaceSuggestion `json:"suggestion,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// clusterAssign is the body of POST /faces/clusters/{id}/assign: the subject every
// face in the cluster is assigned to, by uid or by name.
type clusterAssign struct {
	SubjectUID  string `json:"subject_uid,omitempty"`
	SubjectName string `json:"subject_name,omitempty"`
}

// clusterRemoveFace is the body of POST /faces/clusters/{id}/remove-face: the one
// face that does not belong with the rest.
type clusterRemoveFace struct {
	PhotoUID  string `json:"photo_uid"`
	FaceIndex int    `json:"face_index"`
}

// ClusterAssignResult is what naming a whole cluster produced: the subject and one
// marker per member face. The cluster itself is consumed by the assignment.
type ClusterAssignResult struct {
	ClusterUID string   `json:"cluster_uid"`
	Subject    Subject  `json:"subject"`
	Markers    []Marker `json:"markers"`
}

// ClusterPage is one page of the cluster listing plus what surrounds it: how
// many clusters are ready to be shown, how many are still being prepared in the
// background, and where the next page starts (nil at the end).
type ClusterPage struct {
	Clusters   []Cluster `json:"clusters"`
	Total      int       `json:"total"`
	Pending    int       `json:"pending"`
	Limit      int       `json:"limit"`
	Offset     int       `json:"offset"`
	NextOffset *int      `json:"next_offset"`
}

// ListClusters fetches one page of GET /faces/clusters and returns the raw JSON
// body: the clusters of unassigned faces that are ready, each with its
// suggestion, plus the paging fields. A non-positive limit or offset is left out
// and the server's default page is served. It needs the editor or admin role.
// Decode it with DecodeClusters (the groups) or DecodeClusterPage (the whole
// page).
//
// Only clusters whose cached summary has been built are listed; the rest are
// reported as `pending` and prepared by the `face_cluster` job, which the
// request schedules.
func (c *Client) ListClusters(ctx context.Context, limit, offset int) (json.RawMessage, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	return c.get(ctx, "/faces/clusters", query)
}

// AssignCluster names every face of one cluster at once via POST
// /faces/clusters/{id}/assign, resolving the subject by uid or — creating it if
// need be — by name.
func (c *Client) AssignCluster(ctx context.Context, clusterUID string, subject SubjectRef) (json.RawMessage, error) {
	if err := requireUID("cluster", clusterUID); err != nil {
		return nil, err
	}
	body := clusterAssign{SubjectUID: subject.UID, SubjectName: subject.Name}
	return c.send(ctx, http.MethodPost, "/faces/clusters/"+url.PathEscape(clusterUID)+"/assign", body)
}

// RemoveClusterFace drops one stray face from a cluster via POST
// /faces/clusters/{id}/remove-face, before the rest is named. The refreshed
// cluster comes back, or a null one when removing the face emptied it.
func (c *Client) RemoveClusterFace(
	ctx context.Context, clusterUID, photoUID string, faceIndex int,
) (json.RawMessage, error) {
	if err := requireUID("cluster", clusterUID); err != nil {
		return nil, err
	}
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	if faceIndex < 0 {
		return nil, fmt.Errorf("%w: %d", ErrNegativeFaceIndex, faceIndex)
	}
	body := clusterRemoveFace{PhotoUID: photoUID, FaceIndex: faceIndex}
	return c.send(ctx, http.MethodPost, "/faces/clusters/"+url.PathEscape(clusterUID)+"/remove-face", body)
}

// DecodeClusters decodes the clusters out of one page of GET /faces/clusters.
func DecodeClusters(raw json.RawMessage) ([]Cluster, error) {
	page, err := DecodeClusterPage(raw)
	if err != nil {
		return nil, err
	}
	return page.Clusters, nil
}

// DecodeClusterPage decodes one whole page of GET /faces/clusters — the clusters
// together with the totals and the next offset.
func DecodeClusterPage(raw json.RawMessage) (ClusterPage, error) {
	var page ClusterPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return ClusterPage{}, fmt.Errorf("decoding the cluster list: %w", err)
	}
	return page, nil
}

// DecodeClusterAssign decodes the result of naming a whole cluster.
func DecodeClusterAssign(raw json.RawMessage) (ClusterAssignResult, error) {
	var result ClusterAssignResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return ClusterAssignResult{}, fmt.Errorf("decoding the cluster assignment: %w", err)
	}
	return result, nil
}

// DecodeClusterRemoval decodes the {"cluster": …} body of remove-face, returning
// nil when the removal emptied the cluster and the server deleted it.
func DecodeClusterRemoval(raw json.RawMessage) (*Cluster, error) {
	var payload struct {
		Cluster *Cluster `json:"cluster"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the cluster: %w", err)
	}
	return payload.Cluster, nil
}
