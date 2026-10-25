package api

type (
	ChangePlayerUserRequest  int
	CheckLatencyUserResponse []string
	CheckLatencyUserRequest  map[string]int64
	GameStartUserRequest     struct {
		GameName    string `json:"game_name"`
		RoomId      string `json:"room_id"`
		RecordUser  string `json:"record_user,omitempty"`
		PlayerIndex int    `json:"player_index"`
		Record      bool   `json:"record,omitempty"`
		// Players > 0 makes a new room a cabinet with that many player slots:
		// the worker assigns them and everyone else watches.
		Players int `json:"players,omitempty"`
	}
	GameStartUserResponse struct {
		Av      *AppVideoInfo `json:"av"`
		RoomId  string        `json:"roomId"`
		KbMouse bool          `json:"kb_mouse"`
		Slot    *Slot         `json:"slot,omitempty"`
	}
	IceServer struct {
		Urls       string `json:"urls,omitempty"`
		Username   string `json:"username,omitempty"`
		Credential string `json:"credential,omitempty"`
	}
	InitSessionUserResponse struct {
		Wid   string      `json:"wid"`
		Ice   []IceServer `json:"ice"`
		Games []AppMeta   `json:"games"`
	}
	AppMeta struct {
		Alias  string `json:"alias,omitempty"`
		Title  string `json:"title"`
		System string `json:"system"`
	}
	WebrtcSignalUser struct {
		Ice *string `json:"ice,omitempty"`
		Sdp *string `json:"sdp,omitempty"`
	}
	InitUserWebrtcStreamRequest struct {
		Sdp       string `json:"sdp,omitempty"`
		Initiator bool   `json:"initiator"`
	}
)
