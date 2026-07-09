package model

import "time"

type Node struct {
	ID        uint `gorm:"primaryKey"`
	Value     string
	Position  int `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (Node) TableName() string {
	return "nodes"
}

type RouteRule struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"not null"`
	Path      string `gorm:"uniqueIndex;not null"`
	Content   string `gorm:"type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (RouteRule) TableName() string {
	return "route_rules"
}

type Subscription struct {
	ID        uint `gorm:"primaryKey"`
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (Subscription) TableName() string {
	return "subscription"
}
