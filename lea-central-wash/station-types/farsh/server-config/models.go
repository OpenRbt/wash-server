package main

import (
	"time"

	"github.com/OpenRbt/lcw/cmd/storage/pkg/stationtypes/models"
)

type (
	iikoServiceOrder struct {
		Id             int64       `json:"id"`
		StationID      int         `json:"station_id"`
		InternalID     *string     `json:"internal_id,omitempty"`
		ExternalID     *string     `json:"external_id,omitempty"`
		CashDeskNumber *int        `json:"cash_desk_number,omitempty"`
		Amount         float64     `json:"amount"`
		Status         OrderStatus `json:"status"`
		Items          string      `json:"items"`
		CreatedAt      time.Time   `json:"created_at"`
		UpdatedAt      time.Time   `json:"updated_at"`
	}

	iikoServiceMenuCategory struct {
		Id            int64     `json:"id"`
		Code          string    `json:"code"`
		NameRu        string    `json:"display_name_ru"`
		NameKz        string    `json:"display_name_kz"`
		DescriptionRu *string   `json:"description_ru,omitempty"`
		DescriptionKz *string   `json:"description_kz,omitempty"`
		IsActive      bool      `json:"is_active"`
		IsApproved    bool      `json:"is_approved"`
		ImageB64      *string   `json:"image_b64,omitempty"`
		ImageUrl      *string   `json:"image_url,omitempty"`
		Order         int       `json:"sort_order"`
		StationType   string    `json:"station_type"`
		CreatedAt     time.Time `json:"created_at"`
		UpdatedAt     time.Time `json:"updated_at"`
	}

	iikoServiceMenuItem struct {
		Id            int64     `json:"id"`
		Code          string    `json:"code"`
		ExternalID    *string   `json:"external_id,omitempty"`
		Sku           *string   `json:"sku"`
		NameRu        string    `json:"display_name_ru"`
		NameKz        string    `json:"display_name_kz"`
		DescriptionRu *string   `json:"description_ru,omitempty"`
		DescriptionKz *string   `json:"description_kz,omitempty"`
		IsActive      bool      `json:"is_active"`
		IsApproved    bool      `json:"is_approved"`
		IsHidden      *bool     `json:"is_hidden"`
		ImageB64      *string   `json:"picture_b64,omitempty"`
		ImageUrl      *string   `json:"picture_url,omitempty"`
		Price         *float64  `json:"price"`
		Type          string    `json:"type"`
		StationType   string    `json:"station_type"`
		CreatedAt     time.Time `json:"created_at"`
		UpdatedAt     time.Time `json:"updated_at"`
	}

	iikoServiceMinuSize struct {
		Id            int64     `json:"id"`
		Code          string    `json:"code"`
		NameRu        string    `json:"display_name_ru"`
		NameKz        string    `json:"display_name_kz"`
		DescriptionRu *string   `json:"description_ru,omitempty"`
		DescriptionKz *string   `json:"description_kz,omitempty"`
		IsActive      bool      `json:"is_active"`
		IsApproved    bool      `json:"is_approved"`
		StationType   string    `json:"station_type"`
		CreatedAt     time.Time `json:"created_at"`
		UpdatedAt     time.Time `json:"updated_at"`
	}

	iikoServiceMinuCategoryToItem struct {
		CategoryID  int64  `json:"farsh_menu_category_id"`
		ItemID      int64  `json:"farsh_menu_item_id"`
		Order       int    `json:"sort_order"`
		StationType string `json:"station_type"`
	}

	iikoServiceMinuSizeToItem struct {
		ExternalSizeId string  `json:"external_size_id"`
		NameRu         string  `json:"size_name_ru"`
		NameKz         string  `json:"size_name_kz"`
		IsDefault      bool    `json:"is_default"`
		IsHidden       bool    `json:"is_hidden"`
		ItemId         int64   `json:"farsh_menu_item_id"`
		SizeId         int64   `json:"farsh_size_id"`
		Price          float64 `json:"price"`
		Order          int     `json:"sort_order"`
		StationType    string  `json:"station_type"`
	}

	iikoServiceMinuItemToModifier struct {
		ItemDdcID               int64   `json:"farsh_menu_item_dst_id"`
		ItemSrcID               int64   `json:"farsh_menu_item_src_id"`
		AppliesToSizeExternalID string  `json:"applies_to_size_external_id"`
		ExternalGroupID         string  `json:"external_group_id"`
		GroupCode               string  `json:"group_code"`
		GroupNameRu             string  `json:"group_name_ru"`
		GroupNameKz             string  `json:"group_name_kz"`
		HideIfDefault           bool    `json:"hide_if_default"`
		IncludedFree            bool    `json:"included_free"`
		IsDefault               bool    `json:"is_default"`
		MaxQuantity             int     `json:"max_quantity"`
		MinQuantity             int     `json:"min_quantity"`
		GroupMaxQuantity        int     `json:"group_max_quantity"`
		GroupMinQuantity        int     `json:"group_min_quantity"`
		DefaultQuantity         int     `json:"default_quantity"`
		PriceOverride           float64 `json:"price_override"`
		StationType             string  `json:"station_type"`
	}

	iikoServiceStopListEntry struct {
		Id                int64     `json:"id"`
		Key               string    `json:"key"`
		ExternalProductID string    `json:"external_product_id"`
		ExternalSizeID    *string   `json:"external_size_id,omitempty"`
		TerminalGroupID   *string   `json:"terminal_group_id,omitempty"`
		IsStopped         bool      `json:"is_stopped"`
		SourceRevision    *int64    `json:"source_revision,omitempty"`
		ValidTo           *string   `json:"valid_to,omitempty"`
		StationType       string    `json:"station_type"`
		CreatedAt         time.Time `json:"created_at"`
		UpdatedAt         time.Time `json:"updated_at"`
	}

	tempIikoServiceMenuItemModifier struct {
		MaxQuantity int
		MinQuantity int
		GroupName   string
		Modifier    models.IikoServiceMenuItemModifier
	}

	orderItem struct {
		ID        string   `json:"id"`
		Quantity  int      `json:"quantity"`
		SizeId    *string  `json:"size_id,omitempty"`
		Modifiers []string `json:"modifiers"`
	}

	OrderStatus string
)

const (
	OrderStatusNew     OrderStatus = "new"
	OrderStatusFailed  OrderStatus = "failed"
	OrderStatusCreated OrderStatus = "created"
	OrderStatusPaid    OrderStatus = "paid"
	OrderStatusDone    OrderStatus = "done"
)
