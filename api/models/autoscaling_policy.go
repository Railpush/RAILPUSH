package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type AutoscalingPolicy struct {
	ServiceID           string     `json:"service_id"`
	Enabled             bool       `json:"enabled"`
	MinInstances        int        `json:"min_instances"`
	MaxInstances        int        `json:"max_instances"`
	CPUTargetPercent    int        `json:"cpu_target_percent"`
	MemoryTargetPercent int        `json:"memory_target_percent"`
	ScaleOutCooldownSec int        `json:"scale_out_cooldown_sec"`
	ScaleInCooldownSec  int        `json:"scale_in_cooldown_sec"`
	LastScaledAt        *time.Time `json:"last_scaled_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func GetAutoscalingPolicy(serviceID string) (*AutoscalingPolicy, error) {
	var p AutoscalingPolicy
	err := database.DB.QueryRow(
		`SELECT service_id, COALESCE(enabled,false), COALESCE(min_instances,1), COALESCE(max_instances,1), COALESCE(cpu_target_percent,70), COALESCE(memory_target_percent,75), COALESCE(scale_out_cooldown_sec,120), COALESCE(scale_in_cooldown_sec,180), last_scaled_at, created_at, updated_at
		   FROM service_autoscaling_policies WHERE service_id=$1`,
		serviceID,
	).Scan(
		&p.ServiceID, &p.Enabled, &p.MinInstances, &p.MaxInstances, &p.CPUTargetPercent, &p.MemoryTargetPercent, &p.ScaleOutCooldownSec, &p.ScaleInCooldownSec, &p.LastScaledAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func UpsertAutoscalingPolicy(p *AutoscalingPolicy) error {
	return database.DB.QueryRow(
		`INSERT INTO service_autoscaling_policies
		    (service_id, enabled, min_instances, max_instances, cpu_target_percent, memory_target_percent, scale_out_cooldown_sec, scale_in_cooldown_sec, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW(),NOW())
		 ON CONFLICT (service_id)
		 DO UPDATE SET enabled=EXCLUDED.enabled, min_instances=EXCLUDED.min_instances, max_instances=EXCLUDED.max_instances, cpu_target_percent=EXCLUDED.cpu_target_percent, memory_target_percent=EXCLUDED.memory_target_percent, scale_out_cooldown_sec=EXCLUDED.scale_out_cooldown_sec, scale_in_cooldown_sec=EXCLUDED.scale_in_cooldown_sec, updated_at=NOW()
		 RETURNING created_at, updated_at, last_scaled_at`,
		p.ServiceID, p.Enabled, p.MinInstances, p.MaxInstances, p.CPUTargetPercent, p.MemoryTargetPercent, p.ScaleOutCooldownSec, p.ScaleInCooldownSec,
	).Scan(&p.CreatedAt, &p.UpdatedAt, &p.LastScaledAt)
}

func ListEnabledAutoscalingPolicies() ([]AutoscalingPolicy, error) {
	rows, err := database.DB.Query(
		`SELECT service_id, enabled, COALESCE(min_instances,1), COALESCE(max_instances,1), COALESCE(cpu_target_percent,70), COALESCE(memory_target_percent,75), COALESCE(scale_out_cooldown_sec,120), COALESCE(scale_in_cooldown_sec,180), last_scaled_at, created_at, updated_at
		   FROM service_autoscaling_policies WHERE enabled=TRUE`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutoscalingPolicy
	for rows.Next() {
		var p AutoscalingPolicy
		if err := rows.Scan(&p.ServiceID, &p.Enabled, &p.MinInstances, &p.MaxInstances, &p.CPUTargetPercent, &p.MemoryTargetPercent, &p.ScaleOutCooldownSec, &p.ScaleInCooldownSec, &p.LastScaledAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func TouchAutoscalingScaledAt(serviceID string, when time.Time) error {
	_, err := database.DB.Exec(
		"UPDATE service_autoscaling_policies SET last_scaled_at=$1, updated_at=NOW() WHERE service_id=$2",
		when, serviceID,
	)
	return err
}
