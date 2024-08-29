package redis_lock

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"redis-lock/mocks"
	"testing"
	"time"
)

func TestClient_TryLock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	testCases := []struct {
		name string

		mock func() redis.Cmdable
		// 输入
		key        string
		expiration time.Duration

		//输出
		wantErr  error
		wantLock *Lock
	}{
		{
			name:       "locked",
			key:        "locked-key",
			expiration: time.Minute,
			mock: func() redis.Cmdable {
				rdb := mocks.NewMockCmdable(ctrl)
				res := redis.NewBoolResult(true, nil)
				rdb.EXPECT().SetNX(gomock.Any(), "locked-key", gomock.Any(), time.Minute).
					Return(res)
				return rdb
			},
			wantLock: &Lock{
				key: "locked-key",
			},
		},
		{
			name:       "net work error",
			key:        "net work error",
			expiration: time.Minute,
			mock: func() redis.Cmdable {
				rdb := mocks.NewMockCmdable(ctrl)
				res := redis.NewBoolResult(false, errors.New("网络错误"))
				rdb.EXPECT().SetNX(gomock.Any(), "net work error", gomock.Any(), time.Minute).
					Return(res)
				return rdb
			},
			wantErr: errors.New("网络错误"),
			wantLock: &Lock{
				key: "locked-key",
			},
		},

		{
			name:       "failed to lock",
			key:        "failed-key",
			expiration: time.Minute,
			mock: func() redis.Cmdable {
				rdb := mocks.NewMockCmdable(ctrl)
				res := redis.NewBoolResult(false, nil)
				rdb.EXPECT().SetNX(gomock.Any(), "failed-key", gomock.Any(), time.Minute).
					Return(res)
				return rdb
			},
			wantErr: ErrFailedToPreemptLock,
			wantLock: &Lock{
				key: "locked-key",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rdb := tc.mock()
			c := NewClient(rdb)
			l, err := c.TryLock(context.Background(), tc.key, tc.expiration)

			assert.Equal(t, tc.wantErr, err)
			if err != nil {
				return
			}
			assert.NotNil(t, l.client)
			assert.Equal(t, tc.wantLock.key, l.key)
			assert.NotEmpty(t, l.val)
		})
	}
}
