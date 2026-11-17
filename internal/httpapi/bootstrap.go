package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	incusapi "github.com/lxc/incus/v7/shared/api"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentgw"
	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
	"github.com/mickeyzzc/incus-fleet/internal/incusbootstrap"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type bootstrapRequest struct {
	Mode          string   `json:"mode"`
	Address       string   `json:"address"`
	StorageSource string   `json:"storage_source"`
	NetworkCIDR   string   `json:"network_cidr"`
	InstallMode   string   `json:"install_mode"`
	RepoURL       string   `json:"repo_url"`
	RepoKeyURL    string   `json:"repo_key_url"`
	VersionPin    string   `json:"version_pin"`
	Packages      []string `json:"packages"`
}

// agentBootstrap builds, signs, and dispatches an Incus bootstrap spec.
func (a *API) agentBootstrap(w http.ResponseWriter, r *http.Request) {
	if a.agent == nil || a.bootstrap == nil || a.store == nil || a.clusters == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent bootstrap is not enabled")
		return
	}

	var req bootstrapRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Mode = strings.TrimSpace(req.Mode)
	if req.Mode == "" {
		req.Mode = "join"
	}
	req.Address = strings.TrimSpace(req.Address)
	if req.Address == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "address is required")
		return
	}
	if req.InstallMode == "" {
		req.InstallMode = "existing"
	}
	if req.InstallMode != "existing" && req.InstallMode != "package" {
		writeError(w, http.StatusBadRequest, "invalid_request", "install_mode must be existing or package")
		return
	}

	agent, err := a.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	var preseed string
	switch req.Mode {
	case "join":
		preseed, err = a.joinPreseed(r, agent.Cluster, agent.Member, req)
	case "init":
		preseed, err = incusbootstrap.Init(incusbootstrap.InitOptions{
			Member: agent.Member, Address: req.Address,
			StorageSource: req.StorageSource, NetworkCIDR: req.NetworkCIDR,
		})
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "mode must be join or init")
		return
	}
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	taskID := store.NewID()
	nonce, err := randomNonce()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate bootstrap nonce")
		return
	}
	spec := &bootstrap.Spec{
		Version:       1,
		Mode:          req.Mode,
		Cluster:       agent.Cluster,
		Member:        agent.Member,
		Address:       req.Address,
		StorageSource: req.StorageSource,
		NetworkCIDR:   req.NetworkCIDR,
		Preseed:       preseed,
		InstallMode:   req.InstallMode,
		RepoURL:       req.RepoURL,
		RepoKeyURL:    req.RepoKeyURL,
		VersionPin:    req.VersionPin,
		Packages:      req.Packages,
		TaskID:        taskID,
		Nonce:         nonce,
		ExpiresAt:     time.Now().Add(15 * time.Minute).Unix(),
	}
	if err := a.bootstrap.SignBootstrap(spec); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to sign bootstrap spec")
		return
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to encode bootstrap spec")
		return
	}

	result, err := a.agent.Dispatch(r.Context(), agent.ID, agentpb.TaskType_TASK_TYPE_INCUS_BOOTSTRAP, payload, 10*time.Minute)
	if err != nil {
		if errors.Is(err, agentgw.ErrAgentOffline) {
			writeError(w, http.StatusServiceUnavailable, "agent_offline", "agent is not connected")
			return
		}
		writeError(w, http.StatusGatewayTimeout, "agent_timeout", err.Error())
		return
	}
	if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED {
		message := result.Error
		if message == "" {
			message = "agent bootstrap failed"
		}
		writeError(w, http.StatusBadGateway, "agent_error", message)
		return
	}

	var applied agentpb.IncusBootstrapResult
	if err := proto.Unmarshal(result.Payload, &applied); err != nil {
		writeError(w, http.StatusBadGateway, "agent_error", "agent returned an invalid bootstrap result")
		return
	}
	if !applied.Ok {
		writeError(w, http.StatusBadGateway, "agent_error", applied.Message)
		return
	}
	payloadJSON, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(&applied)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to encode bootstrap result")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"result": json.RawMessage(payloadJSON)})
}

func (a *API) joinPreseed(_ *http.Request, cluster, member string, req bootstrapRequest) (string, error) {
	conn, err := a.clusters.Connection(cluster)
	if err != nil {
		return "", err
	}
	client, err := conn.Client()
	if err != nil {
		return "", err
	}
	info, _, err := client.GetCluster()
	if err != nil {
		return "", err
	}
	op, err := client.CreateClusterMember(incusapi.ClusterMembersPost{ServerName: member})
	if err != nil {
		return "", err
	}
	tokenAPI := op.Get()
	token, err := tokenAPI.ToClusterJoinToken()
	if err != nil {
		return "", err
	}
	memberConfig := make([]incusbootstrap.MemberConfig, 0, len(info.MemberConfig))
	for _, key := range info.MemberConfig {
		memberConfig = append(memberConfig, incusbootstrap.MemberConfig{
			Entity: key.Entity, Name: key.Name, Key: key.Key, Value: key.Value,
		})
	}

	return incusbootstrap.Join(incusbootstrap.JoinOptions{
		Member: member, Address: req.Address, StorageSource: req.StorageSource,
		Token: token.String(), MemberConfig: memberConfig,
	})
}

func randomNonce() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
