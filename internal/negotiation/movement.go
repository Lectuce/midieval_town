package negotiation

import (
	"context"
	"errors"
	"log"
	"time"

	"cybertown/internal/game"
)

// 一个世界只启动一个游戏时钟。
// 不与等待模型的 Run/processOne 共用同一个循环。
func (s *Service) RunMovement(
	ctx context.Context,
	worldID string,
) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	last := time.Now()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			now := time.Now()
			elapsed := now.Sub(last)

			if elapsed <= 0 {
				continue
			}

			tickCtx, cancel := context.WithTimeout(
				ctx,
				5*time.Second,
			)

			world, err := game.GetWorld(
				tickCtx, s.DB, worldID,
			)

			if err == nil {
				_, err = game.AdvanceWorldTime(
					tickCtx,
					s.DB,
					worldID,
					elapsed,
					world.Revision,
				)
			}

			cancel()

			if err != nil {
				if ctx.Err() != nil {
					return
				}

				if errors.Is(err, game.ErrWorldRevisionConflict) {
					// 本次未提交，下一次重新读取版本。
					continue
				}

				// 数据或数据库错误不无限刷屏重试。
				log.Printf("world movement clock stopped: %v", err)
				return
			}

			// 只有事务成功才推进本地计时基准。
			last = now
		}
	}
}
