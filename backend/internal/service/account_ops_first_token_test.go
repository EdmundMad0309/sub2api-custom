package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeFirstTokenRepo struct {
	samples     []FirstTokenAccountSample
	setCalls    []schedulableCall
	listErr     error
	setErr      error
	lastGroupID []int64
	lastSince   time.Time
}

type schedulableCall struct {
	accountID   int64
	schedulable bool
}

func (f *fakeFirstTokenRepo) ListFirstTokenSamples(_ context.Context, groupIDs []int64, since time.Time) ([]FirstTokenAccountSample, error) {
	f.lastGroupID = groupIDs
	f.lastSince = since
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.samples, nil
}

func (f *fakeFirstTokenRepo) SetAccountSchedulable(_ context.Context, accountID int64, schedulable bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.setCalls = append(f.setCalls, schedulableCall{accountID: accountID, schedulable: schedulable})
	return nil
}

func newFirstTokenMonitorForTest(repo FirstTokenMonitorRepository, cfg FirstTokenMonitorConfig) *FirstTokenMonitorService {
	svc := NewFirstTokenMonitorService(nil, repo)
	svc.mu.Lock()
	svc.config = cfg
	svc.mu.Unlock()
	return svc
}

func TestValidateFirstTokenMonitorConfig(t *testing.T) {
	cfg := FirstTokenMonitorConfig{
		GroupIDs:         []int64{7, 3, 7, 0 + 2},
		WindowMinutes:    30,
		MinSamples:       3,
		OpenThresholdMS:  7000,
		CloseThresholdMS: 10000,
		IntervalSeconds:  120,
	}
	require.NoError(t, ValidateFirstTokenMonitorConfig(&cfg))
	require.Equal(t, []int64{2, 3, 7}, cfg.GroupIDs, "分组应去重并排序")

	bad := cfg
	bad.CloseThresholdMS = bad.OpenThresholdMS
	require.Error(t, ValidateFirstTokenMonitorConfig(&bad), "关闭阈值必须大于开启阈值")

	bad = cfg
	bad.IntervalSeconds = 5
	require.Error(t, ValidateFirstTokenMonitorConfig(&bad), "检测间隔过小应被拒绝")

	bad = cfg
	bad.WindowMinutes = 0
	require.Error(t, ValidateFirstTokenMonitorConfig(&bad), "窗口必须为正")
}

func TestFirstTokenMonitorRunOnceTogglesSchedulability(t *testing.T) {
	cases := []struct {
		name           string
		averageMS      float64
		samples        int
		schedulable    bool
		expectedCalls  int
		expectedEnable bool
	}{
		{name: "平均 6s 未调度 -> 自动开启", averageMS: 6000, samples: 5, schedulable: false, expectedCalls: 1, expectedEnable: true},
		{name: "平均 7s 边界 -> 自动开启", averageMS: 7000, samples: 5, schedulable: false, expectedCalls: 1, expectedEnable: true},
		{name: "平均 12s 在调度 -> 自动关闭", averageMS: 12000, samples: 5, schedulable: true, expectedCalls: 1, expectedEnable: false},
		{name: "平均 10s 边界 -> 保持现状", averageMS: 10000, samples: 5, schedulable: true, expectedCalls: 0},
		{name: "平均 8.5s 滞回区 -> 保持现状", averageMS: 8500, samples: 5, schedulable: false, expectedCalls: 0},
		{name: "样本不足 -> 跳过", averageMS: 3000, samples: 2, schedulable: true, expectedCalls: 0},
		{name: "无样本 -> 跳过", averageMS: 0, samples: 0, schedulable: true, expectedCalls: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeFirstTokenRepo{samples: []FirstTokenAccountSample{{
				AccountID:   11,
				AccountName: "acc-11",
				Status:      "active",
				Schedulable: tc.schedulable,
				AverageMS:   tc.averageMS,
				Samples:     tc.samples,
			}}}
			svc := newFirstTokenMonitorForTest(repo, FirstTokenMonitorConfig{
				Enabled:          true,
				GroupIDs:         []int64{5},
				WindowMinutes:    30,
				MinSamples:       3,
				OpenThresholdMS:  7000,
				CloseThresholdMS: 10000,
				IntervalSeconds:  120,
			})

			events, samples, err := svc.RunOnce(context.Background())
			require.NoError(t, err)
			require.Len(t, repo.setCalls, tc.expectedCalls)
			require.Len(t, events, tc.expectedCalls)
			require.Len(t, samples, 1)
			require.Equal(t, []int64{5}, repo.lastGroupID)
			require.WithinDuration(t, time.Now().Add(-30*time.Minute), repo.lastSince, time.Minute)

			if tc.expectedCalls == 1 {
				require.Equal(t, int64(11), repo.setCalls[0].accountID)
				require.Equal(t, tc.expectedEnable, repo.setCalls[0].schedulable)
				expectedAction := "scheduling_enabled"
				if !tc.expectedEnable {
					expectedAction = "scheduling_disabled"
				}
				require.Equal(t, expectedAction, events[0].Action)
			}
		})
	}
}

func TestFirstTokenMonitorRunOnceWithoutGroups(t *testing.T) {
	repo := &fakeFirstTokenRepo{}
	svc := newFirstTokenMonitorForTest(repo, FirstTokenMonitorConfig{
		Enabled:          true,
		WindowMinutes:    30,
		MinSamples:       3,
		OpenThresholdMS:  7000,
		CloseThresholdMS: 10000,
		IntervalSeconds:  120,
	})
	events, samples, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)
	require.Empty(t, samples)
	require.Empty(t, repo.setCalls)
}
