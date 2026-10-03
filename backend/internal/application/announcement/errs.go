package announcement

import "github.com/lieyanc/FireCortex/backend/internal/shared/apperr"

var (
	// ErrInvalidAnnouncement 表示公告配置非法。
	ErrInvalidAnnouncement = apperr.New("request.invalid_announcement", "invalid announcement")
	// ErrAnnouncementNotFound 表示公告不存在。
	ErrAnnouncementNotFound = apperr.New("announcement.not_found", "announcement not found")
)
