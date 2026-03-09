package jwt

type Claims struct {
	UserId      string `json:"userId,omitempty"`      // UserID in UUID format
	TrackId     string `json:"trackId,omitempty"`     // TrackID in UUID format
	OrganizerId string `json:"organizerId,omitempty"` // OrganizerID in UUID format
	ActivityId  string `json:"activityId,omitempty"`  // ActivityID in UUID format
	IsAuth      bool   `json:"isAuth,omitempty"`      // Indicates if the token is verified in others services, to avoid multiple verifications in the same request
	Exp         int64  `json:"exp,omitempty"`
	Iat         int64  `json:"iat,omitempty"`
}
