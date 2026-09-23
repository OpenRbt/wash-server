package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	_ "image/jpeg"

	"github.com/IIArneII/go-utils"
	"github.com/OpenRbt/lcw/cmd/storage/pkg/stationtypes"
	"github.com/OpenRbt/lcw/cmd/storage/pkg/stationtypes/models"
	"github.com/google/uuid"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	imageTargetWidth  = 425
	imageTargetHeight = 365
)

type plugin struct {
	repo stationtypes.StationTypePluginRepo
	pub  stationtypes.StationTypePluginPublisher
}

var _ = stationtypes.StationTypePlugin(&plugin{})

var _ = stationtypes.NewStationTypePlugin(New)

func New(сtx context.Context, repo stationtypes.StationTypePluginRepo) (stationtypes.StationTypePlugin, error) {
	return &plugin{
		repo: repo,
	}, nil
}

func (p *plugin) Config(ctx context.Context) (models.PluginConfig, error) {
	return models.PluginConfig{
		Name:    stationType,
		Version: pluginVersion,
		Services: models.PluginServices{
			IikoService: true,
		},
	}, nil
}

func (p *plugin) Start(ctx context.Context) error {
	return nil
}

func (p *plugin) Stop(ctx context.Context) error {
	return nil
}

func (p *plugin) SetPublisher(ctx context.Context, pub stationtypes.StationTypePluginPublisher) error {
	p.pub = pub
	return nil
}

func (p *plugin) Invoke(ctx context.Context, stationId int, methodName string, args map[string]any) (map[string]any, error) {
	switch methodName {
	case "create-order":
		res, err := p.InvokeCreateOrder(ctx, stationId, args)
		if err != nil {
			return nil, fmt.Errorf("failed to create order: %w", err)
		}
		if err, ok := res["error"]; ok {
			fmt.Printf("failed to create order for station %d: %s\n", stationId, err)
		}

		return res, nil

	case "create-and-pay-order":
		res, err := p.InvokeCreateAndPayOrder(ctx, stationId, args)
		if err != nil {
			return nil, fmt.Errorf("failed to create and pay order: %w", err)
		}
		if err, ok := res["error"]; ok {
			fmt.Printf("failed to create and pay order for station %d: %s\n", stationId, err)
		}

		return res, nil

	case "check-order":
		res, err := p.InvokeCheckOrder(ctx, stationId, args)
		if err != nil {
			return nil, fmt.Errorf("failed to check order: %w", err)
		}
		if err, ok := res["error"]; ok {
			fmt.Printf("failed to check order for station %d: %s\n", stationId, err)
		}

		return res, nil

	case "pay-order":
		res, err := p.InvokePayOrder(ctx, stationId, args)
		if err != nil {
			return nil, fmt.Errorf("failed to pay order: %w", err)
		}
		if err, ok := res["error"]; ok {
			fmt.Printf("failed to pay order for station %d: %s\n", stationId, err)
		}

		return res, nil

	default:
		return map[string]any{"error": "method not found"}, nil
	}
}

func (p *plugin) InvokePayOrder(ctx context.Context, stationId int, args map[string]any) (map[string]any, error) {
	orderId, ok := args["orderId"]
	if !ok {
		return map[string]any{"error": "orderId not found"}, nil
	}

	orderIdValue, ok := orderId.(json.Number)
	if !ok {
		return map[string]any{"error": fmt.Sprintf("orderId is not number: %T", orderId)}, nil
	}

	orderIdInt, err := orderIdValue.Int64()
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to parse orderId: %w", err).Error()}, nil
	}

	rawOrder, err := p.repo.GetConfigEntityRow(ctx, tableOrders, orderIdInt)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to get order %d: %w", orderIdInt, err).Error()}, nil
	}

	order, err := fromDB[iikoServiceOrder](rawOrder)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to parse order: %w", err).Error()}, nil
	}

	order.Status = OrderStatusPaid

	rawOrder, err = toDB(order)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to marshal order: %w", err).Error()}, nil
	}

	_, err = p.repo.UpsertConfigEntityRow(ctx, tableOrders, order.Id, rawOrder)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to update order: %w", err).Error()}, nil
	}

	err = p.pub.PayOrder(ctx, models.ServiceOrderPaymentRequest{
		InternalID: order.Id,
		Amount:     order.Amount,
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to pay order: %w", err).Error()}, nil
	}

	fmt.Println("paid iiko order:", order.Id)

	return map[string]any{"orderId": orderIdInt, "status": order.Status}, nil
}

func (p *plugin) InvokeCheckOrder(ctx context.Context, stationId int, args map[string]any) (map[string]any, error) {
	orderId, ok := args["orderId"]
	if !ok {
		return map[string]any{"error": "orderId not found"}, nil
	}

	orderIdValue, ok := orderId.(json.Number)
	if !ok {
		return map[string]any{"error": fmt.Sprintf("orderId is not number: %T", orderId)}, nil
	}

	orderIdInt, err := orderIdValue.Int64()
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to parse orderId: %w", err).Error()}, nil
	}

	rawOrder, err := p.repo.GetConfigEntityRow(ctx, tableOrders, orderIdInt)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to get order %d: %w", orderIdInt, err).Error()}, nil
	}

	order, err := fromDB[iikoServiceOrder](rawOrder)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to parse order: %w", err).Error()}, nil
	}

	returnStatus := string(order.Status)
	if (order.Status == OrderStatusPaid || order.Status == OrderStatusRetry) && order.ExternalStatus != nil {
		returnStatus = *order.ExternalStatus
	}

	return map[string]any{"orderId": orderIdInt, "status": returnStatus, "cashDeskNumber": order.CashDeskNumber}, nil
}

func (p *plugin) InvokeCreateOrder(ctx context.Context, stationId int, args map[string]any) (map[string]any, error) {
	if p.pub == nil {
		return map[string]any{"error": "publisher is not set"}, nil
	}

	amount, ok := args["amount"]
	if !ok {
		return map[string]any{"error": "amount not found"}, nil
	}

	amountValue, ok := amount.(json.Number)
	if !ok {
		return map[string]any{"error": fmt.Sprintf("amount is not number: %T", amount)}, nil
	}

	amountFloat, err := amountValue.Float64()
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to parse amount: %w", err).Error()}, nil
	}

	delete(args, "amount")

	orderPlace := ""
	if op, ok := args["orderPlace"]; ok {
		if opStr, ok := op.(string); ok {
			orderPlace = opStr
		}
		delete(args, "orderPlace")
	}

	if len(args) == 0 {
		return map[string]any{"error": "no items"}, nil
	}

	items := make([]orderItem, 0, len(args))
	for key, value := range args {
		quantity, ok := value.(json.Number)
		if !ok {
			return map[string]any{"error": "quantity is not number"}, nil
		}

		quantityInt, err := quantity.Int64()
		if err != nil {
			return map[string]any{"error": fmt.Errorf("failed to parse quantity: %w", err).Error()}, nil
		}

		item, err := parseOrderItem(key, int(quantityInt))
		if err != nil {
			return map[string]any{"error": fmt.Errorf("failed to parse order item: %w", err).Error()}, nil
		}

		items = append(items, item)
	}

	lastOrders, _, err := p.repo.ListConfigEntityRows(ctx, tableOrders, models.ListParams{
		Limit:  1,
		SortBy: "-id",
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to get last orders: %w", err).Error()}, nil
	}

	maxOrderId := int64(0)
	if len(lastOrders) > 0 {
		lastOrder, err := fromDB[iikoServiceOrder](lastOrders[0])
		if err != nil {
			return map[string]any{"error": fmt.Errorf("failed to fromDB: %w", err).Error()}, nil
		}

		maxOrderId = lastOrder.Id
	}

	maxOrderId++

	itemsJson, err := json.Marshal(items)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to marshal items: %w", err).Error()}, nil
	}

	now := time.Now().UTC()
	m, err := toDB(iikoServiceOrder{
		Id:         maxOrderId,
		StationID:  stationId,
		InternalID: nil,
		ExternalID: nil,
		Items:      string(itemsJson),
		Amount:     amountFloat,
		Status:     OrderStatusNew,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to toDB: %w", err).Error()}, nil
	}

	_, err = p.repo.UpsertConfigEntityRow(ctx, tableOrders, maxOrderId, m)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to upsert order: %w", err).Error()}, nil
	}

	orderItems, err := utils.SelectE(items, func(item orderItem) (models.ServiceOrderItemCreationRequest, error) {
		iduuid, err := uuid.Parse(item.ID)
		if err != nil {
			return models.ServiceOrderItemCreationRequest{}, fmt.Errorf("failed to parse uuid: %w", err)
		}

		var sizeId *uuid.UUID
		if item.SizeId != nil {
			sizeIduuid, err := uuid.Parse(*item.SizeId)
			if err != nil {
				return models.ServiceOrderItemCreationRequest{}, fmt.Errorf("failed to parse uuid: %w", err)
			}
			sizeId = &sizeIduuid
		}

		modifiers, err := utils.SelectE(item.Modifiers, func(m string) (models.ServiceOrderItemCreationModifierRequest, error) {
			id, err := uuid.Parse(m)
			if err != nil {
				return models.ServiceOrderItemCreationModifierRequest{}, fmt.Errorf("failed to parse uuid: %w", err)
			}

			return models.ServiceOrderItemCreationModifierRequest{
				ID:    id,
				Count: 1,
			}, nil
		})
		if err != nil {
			return models.ServiceOrderItemCreationRequest{}, fmt.Errorf("failed to select modifiers: %w", err)
		}

		return models.ServiceOrderItemCreationRequest{
			ID:        iduuid,
			Count:     item.Quantity,
			SizeId:    sizeId,
			Modifiers: modifiers,
		}, nil
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to select items: %w", err).Error()}, nil
	}

	err = p.pub.CreateOrder(ctx, models.ServiceOrderCreationRequest{
		InternalID: maxOrderId,
		StationID:  stationId,
		Price:      amountFloat,
		Items:      orderItems,
		OrderPlace: orderPlace,
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to create order: %w", err).Error()}, nil
	}

	fmt.Println("created iiko order:", maxOrderId)

	return map[string]any{"orderId": maxOrderId}, nil
}

func (p *plugin) InvokeCreateAndPayOrder(ctx context.Context, stationId int, args map[string]any) (map[string]any, error) {
	if p.pub == nil {
		return map[string]any{"error": "publisher is not set"}, nil
	}

	amount, ok := args["amount"]
	if !ok {
		return map[string]any{"error": "amount not found"}, nil
	}

	amountValue, ok := amount.(json.Number)
	if !ok {
		return map[string]any{"error": fmt.Sprintf("amount is not number: %T", amount)}, nil
	}

	amountFloat, err := amountValue.Float64()
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to parse amount: %w", err).Error()}, nil
	}

	delete(args, "amount")

	orderPlace := ""
	if op, ok := args["orderPlace"]; ok {
		if opStr, ok := op.(string); ok {
			orderPlace = opStr
		}
		delete(args, "orderPlace")
	}

	// retry flag: when set, the station re-sent an already-paid order after a previous
	// create-and-pay attempt failed. The order is created with status "retry" so the
	// iiko side can dedup against the previous attempt instead of charging twice.
	retry := false
	if r, ok := args["retry"]; ok {
		if b, ok := r.(bool); ok {
			retry = b
		}
		delete(args, "retry")
	}

	if len(args) == 0 {
		return map[string]any{"error": "no items"}, nil
	}

	items := make([]orderItem, 0, len(args))
	for key, value := range args {
		quantity, ok := value.(json.Number)
		if !ok {
			return map[string]any{"error": "quantity is not number"}, nil
		}

		quantityInt, err := quantity.Int64()
		if err != nil {
			return map[string]any{"error": fmt.Errorf("failed to parse quantity: %w", err).Error()}, nil
		}

		item, err := parseOrderItem(key, int(quantityInt))
		if err != nil {
			return map[string]any{"error": fmt.Errorf("failed to parse order item: %w", err).Error()}, nil
		}

		items = append(items, item)
	}

	lastOrders, _, err := p.repo.ListConfigEntityRows(ctx, tableOrders, models.ListParams{
		Limit:  1,
		SortBy: "-id",
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to get last orders: %w", err).Error()}, nil
	}

	maxOrderId := int64(0)
	if len(lastOrders) > 0 {
		lastOrder, err := fromDB[iikoServiceOrder](lastOrders[0])
		if err != nil {
			return map[string]any{"error": fmt.Errorf("failed to fromDB: %w", err).Error()}, nil
		}

		maxOrderId = lastOrder.Id
	}

	maxOrderId++

	itemsJson, err := json.Marshal(items)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to marshal items: %w", err).Error()}, nil
	}

	orderStatus := OrderStatusPaid
	if retry {
		orderStatus = OrderStatusRetry
	}

	externalStatus := string(OrderStatusNew)
	now := time.Now().UTC()
	m, err := toDB(iikoServiceOrder{
		Id:             maxOrderId,
		StationID:      stationId,
		InternalID:     nil,
		ExternalID:     nil,
		Items:          string(itemsJson),
		Amount:         amountFloat,
		Status:         orderStatus,
		ExternalStatus: &externalStatus,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to toDB: %w", err).Error()}, nil
	}

	_, err = p.repo.UpsertConfigEntityRow(ctx, tableOrders, maxOrderId, m)
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to upsert order: %w", err).Error()}, nil
	}

	orderItems, err := utils.SelectE(items, func(item orderItem) (models.ServiceOrderItemCreationRequest, error) {
		iduuid, err := uuid.Parse(item.ID)
		if err != nil {
			return models.ServiceOrderItemCreationRequest{}, fmt.Errorf("failed to parse uuid: %w", err)
		}

		var sizeId *uuid.UUID
		if item.SizeId != nil {
			sizeIduuid, err := uuid.Parse(*item.SizeId)
			if err != nil {
				return models.ServiceOrderItemCreationRequest{}, fmt.Errorf("failed to parse uuid: %w", err)
			}
			sizeId = &sizeIduuid
		}

		modifiers, err := utils.SelectE(item.Modifiers, func(m string) (models.ServiceOrderItemCreationModifierRequest, error) {
			id, err := uuid.Parse(m)
			if err != nil {
				return models.ServiceOrderItemCreationModifierRequest{}, fmt.Errorf("failed to parse uuid: %w", err)
			}

			return models.ServiceOrderItemCreationModifierRequest{
				ID:    id,
				Count: 1,
			}, nil
		})
		if err != nil {
			return models.ServiceOrderItemCreationRequest{}, fmt.Errorf("failed to select modifiers: %w", err)
		}

		return models.ServiceOrderItemCreationRequest{
			ID:        iduuid,
			Count:     item.Quantity,
			SizeId:    sizeId,
			Modifiers: modifiers,
		}, nil
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to select items: %w", err).Error()}, nil
	}

	err = p.pub.CreateAndPayOrder(ctx, models.ServiceOrderCreateAndPayRequest{
		InternalID: maxOrderId,
		StationID:  stationId,
		Price:      amountFloat,
		Amount:     amountFloat,
		Items:      orderItems,
		OrderPlace: orderPlace,
		Retry:      retry,
	})
	if err != nil {
		return map[string]any{"error": fmt.Errorf("failed to create and pay order: %w", err).Error()}, nil
	}

	fmt.Println("created and paid iiko order:", maxOrderId)

	return map[string]any{"orderId": maxOrderId}, nil
}

func (p *plugin) IikoServiceOrderStatusChange(ctx context.Context, status models.IikoServiceOrderStatusChange) error {
	rawOrder, err := p.repo.GetConfigEntityRow(ctx, tableOrders, status.InternalID)
	if err != nil {
		return fmt.Errorf("failed to get items: %w", err)
	}

	order, err := fromDB[iikoServiceOrder](rawOrder)
	if err != nil {
		return fmt.Errorf("failed to select items: %w", err)
	}

	if order.Status == OrderStatusPaid || order.Status == OrderStatusRetry {
		// create-and-pay (and retry) order: track iiko progress in external_status only
		var extStatus string
		if status.Error != nil {
			fmt.Printf("iiko service order %d with status %s has error: %s\n", order.InternalID, status.Status, *status.Error)
			extStatus = string(OrderStatusFailed)
		} else {
			switch status.Status {
			case models.IikoServiceOrderStatusSuccessCreatedInIiko:
				extStatus = string(OrderStatusCreated)
			default:
				return fmt.Errorf("unexpected iiko service order status for paid order: %s", status.Status)
			}
		}
		order.ExternalStatus = &extStatus
	} else {
		if status.Error != nil {
			fmt.Printf("iiko service order %d with status %s has error: %s\n", order.InternalID, status.Status, *status.Error)
			order.Status = OrderStatusFailed
		} else {
			switch status.Status {
			case models.IikoServiceOrderStatusSuccessCreatedInIiko:
				order.Status = OrderStatusCreated
			case models.IikoServiceOrderStatusPaid:
				order.Status = OrderStatusPaid
			default:
				return fmt.Errorf("unexpected iiko service order status: %s", status.Status)
			}
		}
	}

	order.CashDeskNumber = status.CashDeskNumber

	rawOrder, err = toDB(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	_, err = p.repo.UpsertConfigEntityRow(ctx, tableOrders, order.Id, rawOrder)
	if err != nil {
		return fmt.Errorf("failed to upsert order: %w", err)
	}

	return nil
}

func (p *plugin) IikoServiceSendSyncMenuRequest(ctx context.Context) error {
	if p.pub != nil {
		return p.pub.RequestServiceMenu(ctx)
	}

	return nil
}

func (p *plugin) IikoServiceUpdateMenu(ctx context.Context, menu models.IikoServiceMenu) error {
	fmt.Println("start sync menu for", stationType)

	categories := make([]models.IikoServiceMenuCategory, 0, len(menu.Categories))
	items := make([]models.IikoServiceMenuItem, 0, len(menu.Categories)*5)
	modifiers := make([]models.IikoServiceMenuItemModifier, 0, len(menu.Categories)*5)
	sizes := make([]models.IikoServiceMenuItemSize, 0, len(menu.Categories)*10)
	categoriesToItemsMap := make(map[uuid.UUID][]models.IikoServiceMenuItem, len(menu.Categories))
	itemsToSizesMap := make(map[uuid.UUID][]models.IikoServiceMenuItemSize, len(menu.Categories)*5)
	itemsToModifiersMap := make(map[uuid.UUID][]tempIikoServiceMenuItemModifier, len(menu.Categories)*5)

	for _, category := range menu.Categories {
		categories = append(categories, category)

		categoriesToItemsMap[category.ID] = make([]models.IikoServiceMenuItem, 0, len(category.Items))
		for _, item := range category.Items {
			_, ok := utils.Find(items, func(i models.IikoServiceMenuItem) bool {
				return i.ID == item.ID
			})
			if !ok {
				items = append(items, item)
			}

			categoriesToItemsMap[category.ID] = append(categoriesToItemsMap[category.ID], item)

			if len(item.Sizes) > 1 {
				itemsToSizesMap[item.ID] = make([]models.IikoServiceMenuItemSize, 0, len(item.Sizes))
				for _, size := range item.Sizes {
					_, ok := utils.Find(sizes, func(s models.IikoServiceMenuItemSize) bool {
						return s.ID == size.ID
					})
					if !ok {
						sizes = append(sizes, size)
					}

					itemsToSizesMap[item.ID] = append(itemsToSizesMap[item.ID], size)
				}
			}

			if _, exists := itemsToModifiersMap[item.ID]; !exists {
				itemsToModifiersMap[item.ID] = make([]tempIikoServiceMenuItemModifier, 0, len(item.Modifiers))
			}
			for _, modifierGroup := range item.Modifiers {
				for _, modifier := range modifierGroup.Items {
					_, ok := utils.Find(modifiers, func(m models.IikoServiceMenuItemModifier) bool {
						return m.ID == modifier.ID
					})
					if !ok {
						modifiers = append(modifiers, modifier)
					}
					nameKZ, _ := modifierGroup.Names["kk"]
					itemsToModifiersMap[item.ID] = append(itemsToModifiersMap[item.ID], tempIikoServiceMenuItemModifier{
						MaxQuantity: modifierGroup.MaxQuantity,
						MinQuantity: modifierGroup.MinQuantity,
						GroupName:   modifierGroup.Name,
						GroupNameKz: nameKZ,
						Modifier:    modifier,
					})
				}
			}
		}
	}

	if err := p.syncCategories(ctx, categories); err != nil {
		return fmt.Errorf("failed to sync categories: %w", err)
	}
	if err := p.syncItems(ctx, items); err != nil {
		return fmt.Errorf("failed to sync items: %w", err)
	}
	if err := p.syncModifiers(ctx, modifiers); err != nil {
		return fmt.Errorf("failed to sync modifiers: %w", err)
	}
	if err := p.syncSizes(ctx, sizes); err != nil {
		return fmt.Errorf("failed to sync sizes: %w", err)
	}
	if err := p.syncCategoriesToItems(ctx, categoriesToItemsMap); err != nil {
		return fmt.Errorf("failed to sync categories to items: %w", err)
	}
	if err := p.syncItemsToSizes(ctx, itemsToSizesMap); err != nil {
		return fmt.Errorf("failed to sync sizes to items: %w", err)
	}
	if err := p.syncItemsToModifiers(ctx, itemsToModifiersMap); err != nil {
		return fmt.Errorf("failed to sync modifiers to items: %w", err)
	}
	if err := p.syncStopList(ctx, items, modifiers); err != nil {
		return fmt.Errorf("failed to sync stop list: %w", err)
	}

	fmt.Println("end sync menu for", stationType)

	return nil
}

func (p *plugin) syncCategories(ctx context.Context, categories []models.IikoServiceMenuCategory) error {
	rawCurrentCategories, err := p.repo.GetConfigTableRows(ctx, tableCategories)
	if err != nil {
		return fmt.Errorf("failed to get categories: %w", err)
	}
	currentCategories, err := utils.SelectE(rawCurrentCategories, fromDB[iikoServiceMenuCategory])
	if err != nil {
		return fmt.Errorf("failed to select categories: %w", err)
	}
	currentCategoriesMap := utils.Map(currentCategories, func(c iikoServiceMenuCategory) string {
		return c.Code
	})

	maxCategoriesId := int64(0)
	if len(currentCategories) > 0 {
		maxCategoriesId = slices.MaxFunc(utils.Select(currentCategories, func(c iikoServiceMenuCategory) int64 { return c.Id }), func(i int64, maximum int64) int {
			return int(i - maximum)
		})
	}

	// Creating and updating categories
	for categoryIndex, category := range categories {
		nameKZ, _ := category.Names["kk"]
		if currentCategory, ok := currentCategoriesMap[category.ID.String()]; ok {
			if category.Name != currentCategory.NameRu ||
				nameKZ != currentCategory.NameKz ||
				!category.IsHidden != currentCategory.IsActive ||
				categoryIndex != currentCategory.Order {

				currentCategory.NameRu = category.Name
				currentCategory.NameKz = nameKZ
				currentCategory.IsActive = !category.IsHidden
				currentCategory.Order = categoryIndex

				m, err := toDB(currentCategory)
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}

				_, err = p.repo.UpsertConfigEntityRow(ctx, tableCategories, currentCategory.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert category: %w", err)
				}
			}

		} else {
			maxCategoriesId++

			now := time.Now().UTC()
			m, err := toDB(iikoServiceMenuCategory{
				Id:          maxCategoriesId,
				Code:        category.ID.String(),
				NameRu:      category.Name,
				NameKz:      nameKZ,
				IsActive:    !category.IsHidden,
				IsApproved:  true,
				Order:       categoryIndex,
				StationType: stationType,
				CreatedAt:   now,
				UpdatedAt:   now,
			})
			if err != nil {
				return fmt.Errorf("failed to toDB: %w", err)
			}

			_, err = p.repo.UpsertConfigEntityRow(ctx, tableCategories, maxCategoriesId, m)
			if err != nil {
				return fmt.Errorf("failed to upsert category: %w", err)
			}
		}

		delete(currentCategoriesMap, category.ID.String())
	}

	// Hiding non-existent categories
	for _, currentCategory := range currentCategoriesMap {
		if !currentCategory.IsActive {
			continue
		}

		currentCategory.IsActive = false
		m, err := toDB(currentCategory)
		if err != nil {
			return fmt.Errorf("failed to toDB: %w", err)
		}

		_, err = p.repo.UpsertConfigEntityRow(ctx, tableCategories, currentCategory.Id, m)
		if err != nil {
			return fmt.Errorf("failed to upsert category: %w", err)
		}
	}

	return nil
}

func (p *plugin) syncItems(ctx context.Context, items []models.IikoServiceMenuItem) error {
	rawCurrentItems, err := p.repo.GetConfigTableRows(ctx, tableItems)
	if err != nil {
		return fmt.Errorf("failed to get items: %w", err)
	}
	сurrentItems, err := utils.SelectE(rawCurrentItems, fromDB[iikoServiceMenuItem])
	if err != nil {
		return fmt.Errorf("failed to select items: %w", err)
	}
	currentItemsMap := utils.Map(сurrentItems, func(c iikoServiceMenuItem) string {
		return c.Code
	})

	maxItemsId := int64(0)
	if len(сurrentItems) > 0 {
		maxItemsId = slices.MaxFunc(utils.Select(сurrentItems, func(c iikoServiceMenuItem) int64 { return c.Id }), func(i int64, maximum int64) int {
			return int(i - maximum)
		})
	}

	// Creating and updating items
	for _, item := range items {
		nameKZ, _ := item.Names["kk"]
		descriptionKZ, _ := item.Descriptions["kk"]

		if currentItem, ok := currentItemsMap[item.ID.String()]; ok {
			var imageUrl string
			if len(item.Sizes) > 0 {
				imageUrl = item.Sizes[0].ImageUrl
			}

			var price *float64
			if len(item.Sizes) == 1 {
				price = &item.Sizes[0].Price
			}
			if item.Name != currentItem.NameRu ||
				currentItem.NameKz != nameKZ || currentItem.DescriptionKz != &descriptionKZ ||
				!item.IsHidden != currentItem.IsActive ||
				(currentItem.ImageUrl == nil && imageUrl != "") || (currentItem.ImageUrl != nil && imageUrl != *currentItem.ImageUrl) ||
				(currentItem.IsHidden == nil || *currentItem.IsHidden != item.IsHidden) ||
				(currentItem.Sku == nil || *currentItem.Sku != item.Sku) ||
				(currentItem.DescriptionRu == nil || *currentItem.DescriptionRu != item.Description) ||
				(price != nil && (currentItem.Price == nil || *currentItem.Price != *price)) ||
				(currentItem.ExternalID == nil || *currentItem.ExternalID != item.ID.String()) {

				extID := item.ID.String()
				currentItem.NameRu = item.Name
				currentItem.DescriptionRu = &item.Description
				currentItem.NameKz = nameKZ
				currentItem.DescriptionKz = &descriptionKZ
				currentItem.Price = price
				currentItem.Sku = &item.Sku
				currentItem.IsActive = !item.IsHidden
				currentItem.IsHidden = &item.IsHidden
				currentItem.ExternalID = &extID

				if currentItem.ImageUrl == nil || imageUrl != *currentItem.ImageUrl {
					if imageUrl == "" {
						currentItem.ImageUrl = nil
						currentItem.ImageB64 = nil
					} else {
						imageData, err := p.fetchImageB64ByURL(ctx, imageUrl)
						if err != nil {
							fmt.Println("failed to fetchImageB64ByURL:", err.Error())
							currentItem.ImageUrl = nil
							currentItem.ImageB64 = nil
						} else {
							currentItem.ImageUrl = &imageUrl
							currentItem.ImageB64 = &imageData
						}
					}
				}

				m, err := toDB(currentItem)
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}

				_, err = p.repo.UpsertConfigEntityRow(ctx, tableItems, currentItem.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert item: %w", err)
				}
			}

		} else {
			var imageUrl *string
			var imageB64 *string
			if len(item.Sizes) > 0 {
				if url := item.Sizes[0].ImageUrl; url != "" {
					imageData, err := p.fetchImageB64ByURL(ctx, url)
					if err != nil {
						fmt.Println("failed to fetchImageB64ByURL:", err.Error())
					} else {
						imageUrl = &url
						imageB64 = &imageData
					}
				}
			}

			var price *float64
			if len(item.Sizes) == 1 {
				price = &item.Sizes[0].Price
			}

			maxItemsId++

			now := time.Now().UTC()
			extID := item.ID.String()
			m, err := toDB(iikoServiceMenuItem{
				Id:            maxItemsId,
				Code:          item.ID.String(),
				ExternalID:    &extID,
				Sku:           &item.Sku,
				NameRu:        item.Name,
				NameKz:        nameKZ,
				DescriptionRu: &item.Description,
				DescriptionKz: &descriptionKZ,
				Price:         price,
				IsActive:      !item.IsHidden,
				IsHidden:      &item.IsHidden,
				IsApproved:    true,
				ImageB64:      imageB64,
				ImageUrl:      imageUrl,
				Type:          "DISH",
				StationType:   stationType,
				CreatedAt:     now,
				UpdatedAt:     now,
			})
			if err != nil {
				return fmt.Errorf("failed to toDB: %w", err)
			}

			_, err = p.repo.UpsertConfigEntityRow(ctx, tableItems, maxItemsId, m)
			if err != nil {
				return fmt.Errorf("failed to upsert item: %w", err)
			}
		}

		delete(currentItemsMap, item.ID.String())
	}

	// Hiding non-existent items
	for _, currentItem := range currentItemsMap {
		if !currentItem.IsActive || currentItem.Type != "DISH" {
			continue
		}

		currentItem.IsActive = false
		currentItem.IsHidden = utils.P(true)
		m, err := toDB(currentItem)
		if err != nil {
			return fmt.Errorf("failed to toDB: %w", err)
		}

		_, err = p.repo.UpsertConfigEntityRow(ctx, tableItems, currentItem.Id, m)
		if err != nil {
			return fmt.Errorf("failed to upsert item: %w", err)
		}
	}

	return nil
}

func (p *plugin) syncSizes(ctx context.Context, sizes []models.IikoServiceMenuItemSize) error {
	rawCurrentSizes, err := p.repo.GetConfigTableRows(ctx, tableSizes)
	if err != nil {
		return fmt.Errorf("failed to get sizes: %w", err)
	}
	сurrentSizes, err := utils.SelectE(rawCurrentSizes, fromDB[iikoServiceMinuSize])
	if err != nil {
		return fmt.Errorf("failed to select sizes: %w", err)
	}
	currentSizesMap := utils.Map(сurrentSizes, func(c iikoServiceMinuSize) string {
		return c.Code
	})

	maxSizeId := int64(0)
	if len(сurrentSizes) > 0 {
		maxSizeId = slices.MaxFunc(utils.Select(сurrentSizes, func(c iikoServiceMinuSize) int64 { return c.Id }), func(i int64, maximum int64) int {
			return int(i - maximum)
		})
	}

	// Creating and updating sizes
	for _, size := range sizes {
		if currentSize, ok := currentSizesMap[size.ID.String()]; ok {
			if size.Name != currentSize.NameRu ||
				!size.IsHidden != currentSize.IsActive {

				currentSize.NameRu = size.Name
				currentSize.IsActive = !size.IsHidden

				m, err := toDB(currentSize)
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}

				_, err = p.repo.UpsertConfigEntityRow(ctx, tableSizes, currentSize.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert size: %w", err)
				}
			}

		} else {
			maxSizeId++

			now := time.Now().UTC()
			m, err := toDB(iikoServiceMinuSize{
				Id:            maxSizeId,
				Code:          size.ID.String(),
				NameRu:        size.Name,
				NameKz:        "",
				DescriptionRu: nil,
				DescriptionKz: nil,
				IsActive:      !size.IsHidden,
				IsApproved:    true,
				StationType:   stationType,
				CreatedAt:     now,
				UpdatedAt:     now,
			})
			if err != nil {
				return fmt.Errorf("failed to toDB: %w", err)
			}

			_, err = p.repo.UpsertConfigEntityRow(ctx, tableSizes, maxSizeId, m)
			if err != nil {
				return fmt.Errorf("failed to upsert item: %w", err)
			}
		}

		delete(currentSizesMap, size.ID.String())
	}

	// Hiding non-existent items
	for _, currentSize := range currentSizesMap {
		if !currentSize.IsActive {
			continue
		}

		currentSize.IsActive = false
		m, err := toDB(currentSize)
		if err != nil {
			return fmt.Errorf("failed to toDB: %w", err)
		}

		_, err = p.repo.UpsertConfigEntityRow(ctx, tableSizes, currentSize.Id, m)
		if err != nil {
			return fmt.Errorf("failed to upsert size: %w", err)
		}
	}

	return nil
}

func (p *plugin) syncModifiers(ctx context.Context, modifiers []models.IikoServiceMenuItemModifier) error {
	rawCurrentItems, err := p.repo.GetConfigTableRows(ctx, tableItems)
	if err != nil {
		return fmt.Errorf("failed to get items: %w", err)
	}
	сurrentItems, err := utils.SelectE(rawCurrentItems, fromDB[iikoServiceMenuItem])
	if err != nil {
		return fmt.Errorf("failed to select items: %w", err)
	}
	currentItemsMap := utils.Map(сurrentItems, func(c iikoServiceMenuItem) string {
		return c.Code
	})

	maxItemsId := int64(0)
	if len(сurrentItems) > 0 {
		maxItemsId = slices.MaxFunc(utils.Select(сurrentItems, func(c iikoServiceMenuItem) int64 { return c.Id }), func(i int64, maximum int64) int {
			return int(i - maximum)
		})
	}

	// Creating and updating modifiers
	for _, item := range modifiers {
		nameKZ, _ := item.Names["kk"]
		descriptionKZ, _ := item.Descriptions["kk"]

		if currentItem, ok := currentItemsMap[item.ID.String()]; ok {
			if item.Name != currentItem.NameRu ||
				currentItem.NameKz != nameKZ || currentItem.DescriptionKz != &descriptionKZ ||
				!item.IsHidden != currentItem.IsActive ||
				(currentItem.Sku == nil || *currentItem.Sku != item.Sku) ||
				(currentItem.DescriptionRu == nil || *currentItem.DescriptionRu != item.Description) ||
				(currentItem.Price == nil || *currentItem.Price != item.Price) ||
				(currentItem.ExternalID == nil || *currentItem.ExternalID != item.ID.String()) {

				extID := item.ID.String()
				currentItem.NameRu = item.Name
				currentItem.DescriptionRu = &item.Description
				currentItem.NameKz = nameKZ
				currentItem.DescriptionKz = &descriptionKZ
				currentItem.Price = &item.Price
				currentItem.Sku = &item.Sku
				currentItem.IsActive = !item.IsHidden
				currentItem.ExternalID = &extID

				m, err := toDB(currentItem)
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}

				_, err = p.repo.UpsertConfigEntityRow(ctx, tableItems, currentItem.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert item: %w", err)
				}
			}

		} else {
			maxItemsId++

			now := time.Now().UTC()
			extID := item.ID.String()
			m, err := toDB(iikoServiceMenuItem{
				Id:            maxItemsId,
				Code:          item.ID.String(),
				ExternalID:    &extID,
				Sku:           &item.Sku,
				NameRu:        item.Name,
				NameKz:        nameKZ,
				DescriptionRu: &item.Description,
				DescriptionKz: &descriptionKZ,
				Price:         &item.Price,
				IsActive:      !item.IsHidden,
				IsApproved:    true,
				ImageB64:      nil,
				ImageUrl:      nil,
				Type:          "MODIFIER",
				StationType:   stationType,
				CreatedAt:     now,
				UpdatedAt:     now,
			})
			if err != nil {
				return fmt.Errorf("failed to toDB: %w", err)
			}

			_, err = p.repo.UpsertConfigEntityRow(ctx, tableItems, maxItemsId, m)
			if err != nil {
				return fmt.Errorf("failed to upsert item: %w", err)
			}
		}

		delete(currentItemsMap, item.ID.String())
	}

	// Hiding non-existent modifiers
	for _, currentItem := range currentItemsMap {
		if !currentItem.IsActive || currentItem.Type != "MODIFIER" {
			continue
		}

		currentItem.IsActive = false
		currentItem.IsHidden = utils.P(true)
		m, err := toDB(currentItem)
		if err != nil {
			return fmt.Errorf("failed to toDB: %w", err)
		}

		_, err = p.repo.UpsertConfigEntityRow(ctx, tableItems, currentItem.Id, m)
		if err != nil {
			return fmt.Errorf("failed to upsert item: %w", err)
		}
	}

	return nil
}

func (p *plugin) syncCategoriesToItems(ctx context.Context, categoriesToItemsMap map[uuid.UUID][]models.IikoServiceMenuItem) error {
	rawCurrentCategoriesToItems, err := p.repo.GetConfigTableRows(ctx, tableCategoriesToItems)
	if err != nil {
		return fmt.Errorf("failed to get sizes: %w", err)
	}
	сurrentCategoriesToItems, err := utils.SelectE(rawCurrentCategoriesToItems, fromDB[iikoServiceMinuCategoryToItem])
	if err != nil {
		return fmt.Errorf("failed to select sizes: %w", err)
	}
	currentCategoriesToItemsMapByCategoryId := utils.GroupBy(сurrentCategoriesToItems, func(c iikoServiceMinuCategoryToItem) int64 {
		return c.CategoryID
	})

	rawCurrentCategories, err := p.repo.GetConfigTableRows(ctx, tableCategories)
	if err != nil {
		return fmt.Errorf("failed to get categories: %w", err)
	}
	currentCategories, err := utils.SelectE(rawCurrentCategories, fromDB[iikoServiceMenuCategory])
	if err != nil {
		return fmt.Errorf("failed to select categories: %w", err)
	}
	currentCategoriesMapById := utils.Map(currentCategories, func(c iikoServiceMenuCategory) int64 {
		return c.Id
	})
	currentCategoriesMapByCode := utils.Map(currentCategories, func(c iikoServiceMenuCategory) string {
		return c.Code
	})

	rawCurrentItems, err := p.repo.GetConfigTableRows(ctx, tableItems)
	if err != nil {
		return fmt.Errorf("failed to get sizes: %w", err)
	}
	сurrentItems, err := utils.SelectE(rawCurrentItems, fromDB[iikoServiceMenuItem])
	if err != nil {
		return fmt.Errorf("failed to select sizes: %w", err)
	}
	currentItemsMapById := utils.Map(сurrentItems, func(c iikoServiceMenuItem) int64 {
		return c.Id
	})
	currentItemsMapByCode := utils.Map(сurrentItems, func(c iikoServiceMenuItem) string {
		return c.Code
	})

	currentCategoriesToItemsMap := make(map[string][]iikoServiceMinuCategoryToItem, len(currentCategoriesToItemsMapByCategoryId))
	for currentCategoriesToItemsMapIndex, currentCategoriesToItems := range currentCategoriesToItemsMapByCategoryId {
		category, ok := currentCategoriesMapById[currentCategoriesToItemsMapIndex]
		if !ok {
			fmt.Println("category not found", currentCategoriesToItemsMapIndex)
			continue
		}
		currentCategoriesToItemsMap[category.Code] = currentCategoriesToItems
	}
	for сurrentCategoryIndex := range currentCategoriesMapByCode {
		if _, ok := currentCategoriesToItemsMap[сurrentCategoryIndex]; !ok {
			currentCategoriesToItemsMap[сurrentCategoryIndex] = []iikoServiceMinuCategoryToItem{}
		}
	}

	forHide := []iikoServiceMinuCategoryToItem{}
	for categoriesToItemsIndex, categoriesToItems := range categoriesToItemsMap {
		current, ok := currentCategoriesToItemsMap[categoriesToItemsIndex.String()]
		if !ok {
			fmt.Println("category to items not found", categoriesToItemsIndex.String())
			continue
		}

		currentCategory, ok := currentCategoriesMapByCode[categoriesToItemsIndex.String()]
		if !ok {
			fmt.Println("category not found", categoriesToItemsIndex.String())
			continue
		}

		currentMap, err := utils.MapE(current, func(c iikoServiceMinuCategoryToItem) (string, error) {
			currentItem, ok := currentItemsMapById[c.ItemID]
			if !ok {
				return "", fmt.Errorf("item not found: %d", c.ItemID)
			}
			return currentItem.Code, nil
		})
		if err != nil {
			return fmt.Errorf("failed to map items: %w", err)
		}

		for categoryToItemIndex, categoryToItem := range categoriesToItems {
			currentItem, ok := currentItemsMapByCode[categoryToItem.ID.String()]
			if !ok {
				fmt.Println("item not found", categoryToItem.ID.String())
				continue
			}

			if currentCategoryToItem, ok := currentMap[categoryToItem.ID.String()]; ok {
				if categoryToItemIndex != currentCategoryToItem.Order {

					currentCategoryToItem.Order = categoryToItemIndex

					m, err := toDB(currentCategoryToItem)
					if err != nil {
						return fmt.Errorf("failed to toDB: %w", err)
					}

					delete(m, "station_type")
					delete(m, "farsh_menu_category_id")
					delete(m, "farsh_menu_item_id")

					_, err = p.repo.UpsertConfigEntityRelation(ctx, tableCategoriesToItems, "farsh_menu_category_id", currentCategory.Id, "farsh_menu_item_id", currentItem.Id, m)
					if err != nil {
						return fmt.Errorf("failed to upsert category to item: %w", err)
					}
				}
			} else {
				m, err := toDB(iikoServiceMinuCategoryToItem{
					CategoryID:  currentCategory.Id,
					ItemID:      currentItem.Id,
					Order:       categoryToItemIndex,
					StationType: stationType,
				})
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}

				delete(m, "station_type")
				delete(m, "farsh_menu_category_id")
				delete(m, "farsh_menu_item_id")

				_, err = p.repo.UpsertConfigEntityRelation(ctx, tableCategoriesToItems, "farsh_menu_category_id", currentCategory.Id, "farsh_menu_item_id", currentItem.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert category to item: %w", err)
				}
			}

			delete(currentMap, categoryToItem.ID.String())
		}

		for _, current := range currentMap {
			forHide = append(forHide, current)
		}

		delete(currentCategoriesToItemsMap, categoriesToItemsIndex.String())
	}

	for _, categoryToItems := range currentCategoriesToItemsMap {
		for _, current := range categoryToItems {
			forHide = append(forHide, current)
		}
	}

	for _, sizeToItem := range forHide {
		err := p.repo.DeleteConfigEntityRowRelation(ctx, tableCategoriesToItems, "farsh_menu_category_id", sizeToItem.CategoryID, "farsh_menu_item_id", sizeToItem.ItemID)
		if err != nil {
			return fmt.Errorf("failed to delete category to item: %w", err)
		}
	}

	return nil
}

func (p *plugin) syncItemsToSizes(ctx context.Context, itemsToSizesMap map[uuid.UUID][]models.IikoServiceMenuItemSize) error {
	rawCurrentSizesToItems, err := p.repo.GetConfigTableRows(ctx, tableSizesToItems)
	if err != nil {
		return fmt.Errorf("failed to get sizes: %w", err)
	}
	сurrentSizesToItems, err := utils.SelectE(rawCurrentSizesToItems, fromDB[iikoServiceMinuSizeToItem])
	if err != nil {
		return fmt.Errorf("failed to select sizes: %w", err)
	}
	currentSizesToItemsMapByItemId := utils.GroupBy(сurrentSizesToItems, func(c iikoServiceMinuSizeToItem) int64 {
		return c.ItemId
	})

	rawCurrentItems, err := p.repo.GetConfigTableRows(ctx, tableItems)
	if err != nil {
		return fmt.Errorf("failed to get items: %w", err)
	}
	сurrentItems, err := utils.SelectE(rawCurrentItems, fromDB[iikoServiceMenuItem])
	if err != nil {
		return fmt.Errorf("failed to select items: %w", err)
	}
	currentItemsMapById := utils.Map(сurrentItems, func(c iikoServiceMenuItem) int64 {
		return c.Id
	})
	currentItemsMapByCode := utils.Map(сurrentItems, func(c iikoServiceMenuItem) string {
		return c.Code
	})

	rawCurrentSizes, err := p.repo.GetConfigTableRows(ctx, tableSizes)
	if err != nil {
		return fmt.Errorf("failed to get sizes: %w", err)
	}
	сurrentSizes, err := utils.SelectE(rawCurrentSizes, fromDB[iikoServiceMinuSize])
	if err != nil {
		return fmt.Errorf("failed to select sizes: %w", err)
	}
	currentSizesMapByCode := utils.Map(сurrentSizes, func(c iikoServiceMinuSize) string {
		return c.Code
	})

	currentSizesToItemsMap := make(map[string][]iikoServiceMinuSizeToItem, len(currentSizesToItemsMapByItemId))
	for currentSizesToItemsMapIndex, currentSizesToItems := range currentSizesToItemsMapByItemId {
		item, ok := currentItemsMapById[currentSizesToItemsMapIndex]
		if !ok {
			fmt.Println("item not found", currentSizesToItemsMapIndex)
			continue
		}
		currentSizesToItemsMap[item.Code] = currentSizesToItems
	}
	for сurrentItemIndex := range currentItemsMapByCode {
		if _, ok := currentSizesToItemsMap[сurrentItemIndex]; !ok {
			currentSizesToItemsMap[сurrentItemIndex] = []iikoServiceMinuSizeToItem{}
		}
	}

	forHide := []iikoServiceMinuSizeToItem{}
	for itemsToSizesIndex, itemsToSizes := range itemsToSizesMap {
		current, ok := currentSizesToItemsMap[itemsToSizesIndex.String()]
		if !ok {
			fmt.Println("item to sizes not found", itemsToSizesIndex.String())
			continue
		}

		currentItem, ok := currentItemsMapByCode[itemsToSizesIndex.String()]
		if !ok {
			fmt.Println("item not found", itemsToSizesIndex.String())
			continue
		}

		currentMap := utils.Map(current, func(s iikoServiceMinuSizeToItem) string {
			return s.ExternalSizeId
		})

		for itemToSizeIndex, itemToSize := range itemsToSizes {
			currentSize, ok := currentSizesMapByCode[itemToSize.ID.String()]
			if !ok {
				fmt.Println("size not found", itemToSize.ID.String())
				continue
			}

			if currentItemToSize, ok := currentMap[itemToSize.ID.String()]; ok {
				if itemToSize.Name != currentItemToSize.NameRu ||
					itemToSize.IsDefault != currentItemToSize.IsDefault ||
					itemToSize.IsHidden != currentItemToSize.IsHidden ||
					itemToSize.Price != currentItemToSize.Price ||
					itemToSizeIndex != currentItemToSize.Order {

					currentItemToSize.NameRu = itemToSize.Name
					currentItemToSize.IsDefault = itemToSize.IsDefault
					currentItemToSize.IsHidden = itemToSize.IsHidden
					currentItemToSize.Price = itemToSize.Price
					currentItemToSize.Order = itemToSizeIndex

					m, err := toDB(currentItemToSize)
					if err != nil {
						return fmt.Errorf("failed to toDB: %w", err)
					}

					delete(m, "farsh_menu_item_id")
					delete(m, "farsh_size_id")
					delete(m, "station_type")

					_, err = p.repo.UpsertConfigEntityRelation(ctx, tableSizesToItems, "farsh_menu_item_id", currentItem.Id, "farsh_size_id", currentSize.Id, m)
					if err != nil {
						return fmt.Errorf("failed to upsert sizes to items: %w", err)
					}
				}
			} else {
				m, err := toDB(iikoServiceMinuSizeToItem{
					ExternalSizeId: itemToSize.ID.String(),
					NameRu:         itemToSize.Name,
					NameKz:         "",
					IsDefault:      itemToSize.IsDefault,
					IsHidden:       itemToSize.IsHidden,
					Price:          itemToSize.Price,
					Order:          itemToSizeIndex,
					StationType:    stationType,
					ItemId:         currentItem.Id,
					SizeId:         currentSize.Id,
				})
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}

				delete(m, "station_type")
				delete(m, "farsh_menu_item_id")
				delete(m, "farsh_size_id")

				_, err = p.repo.UpsertConfigEntityRelation(ctx, tableSizesToItems, "farsh_menu_item_id", currentItem.Id, "farsh_size_id", currentSize.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert sizes to items: %w", err)
				}
			}

			delete(currentMap, itemToSize.ID.String())
		}

		for _, current := range currentMap {
			forHide = append(forHide, current)
		}

		delete(currentSizesToItemsMap, itemsToSizesIndex.String())
	}

	for _, sizeToItems := range currentSizesToItemsMap {
		for _, current := range sizeToItems {
			forHide = append(forHide, current)
		}
	}

	for _, sizeToItem := range forHide {
		if sizeToItem.IsHidden {
			continue
		}

		sizeToItem.IsHidden = true

		m, err := toDB(sizeToItem)
		if err != nil {
			return fmt.Errorf("failed to toDB: %w", err)
		}

		delete(m, "farsh_menu_item_id")
		delete(m, "farsh_size_id")
		delete(m, "station_type")

		_, err = p.repo.UpsertConfigEntityRelation(ctx, tableSizesToItems, "farsh_menu_item_id", sizeToItem.ItemId, "farsh_size_id", sizeToItem.SizeId, m)
		if err != nil {
			return fmt.Errorf("failed to upsert sizes to items: %w", err)
		}
	}

	return nil
}

func (p *plugin) syncItemsToModifiers(ctx context.Context, itemsToModifiersMap map[uuid.UUID][]tempIikoServiceMenuItemModifier) error {
	rawCurrentItemsToModifiers, err := p.repo.GetConfigTableRows(ctx, tableItemsToModifiers)
	if err != nil {
		return fmt.Errorf("failed to get sizes: %w", err)
	}
	сurrentItemsToModifiers, err := utils.SelectE(rawCurrentItemsToModifiers, fromDB[iikoServiceMinuItemToModifier])
	if err != nil {
		return fmt.Errorf("failed to select sizes: %w", err)
	}
	currentItemsToModifiersMapByItemID := utils.GroupBy(сurrentItemsToModifiers, func(c iikoServiceMinuItemToModifier) int64 {
		return c.ItemSrcID
	})

	rawCurrentItems, err := p.repo.GetConfigTableRows(ctx, tableItems)
	if err != nil {
		return fmt.Errorf("failed to get items: %w", err)
	}
	сurrentItems, err := utils.SelectE(rawCurrentItems, fromDB[iikoServiceMenuItem])
	if err != nil {
		return fmt.Errorf("failed to select items: %w", err)
	}
	currentItemsMapById := utils.Map(сurrentItems, func(c iikoServiceMenuItem) int64 {
		return c.Id
	})
	currentItemsMapByCode := utils.Map(сurrentItems, func(c iikoServiceMenuItem) string {
		return c.Code
	})

	currentItemsToModifiersMap := make(map[string][]iikoServiceMinuItemToModifier, len(currentItemsToModifiersMapByItemID))
	for currentItemsToModifiersMapIndex, currentItemsToModifiers := range currentItemsToModifiersMapByItemID {
		item, ok := currentItemsMapById[currentItemsToModifiersMapIndex]
		if !ok {
			fmt.Println("item not found", currentItemsToModifiersMapIndex)
			continue
		}
		currentItemsToModifiersMap[item.Code] = currentItemsToModifiers
	}
	for сurrentItemIndex := range currentItemsMapByCode {
		if _, ok := currentItemsToModifiersMap[сurrentItemIndex]; !ok {
			currentItemsToModifiersMap[сurrentItemIndex] = []iikoServiceMinuItemToModifier{}
		}
	}

	forHide := []iikoServiceMinuItemToModifier{}
	for itemsToModifiersIndex, itemsToModifiers := range itemsToModifiersMap {
		current, ok := currentItemsToModifiersMap[itemsToModifiersIndex.String()]
		if !ok {
			fmt.Println("items to modifiers not found", itemsToModifiersIndex.String())
			continue
		}

		currentItem, ok := currentItemsMapByCode[itemsToModifiersIndex.String()]
		if !ok {
			fmt.Println("item not found", itemsToModifiersIndex.String())
			continue
		}

		currentMap, err := utils.MapE(current, func(c iikoServiceMinuItemToModifier) (string, error) {
			currentModifierItem, ok := currentItemsMapById[c.ItemDdcID]
			if !ok {
				return "", fmt.Errorf("modifier item not found: %d", c.ItemDdcID)
			}
			return currentModifierItem.Code, nil
		})
		if err != nil {
			return fmt.Errorf("failed to map items: %w", err)
		}

		for _, itemToModifier := range itemsToModifiers {
			currentModifier, ok := currentItemsMapByCode[itemToModifier.Modifier.ID.String()]
			if !ok {
				fmt.Println("modifier not found", itemToModifier.Modifier.ID.String())
				continue
			}

			if currentItemToModifier, ok := currentMap[itemToModifier.Modifier.ID.String()]; ok {
				if itemToModifier.GroupName != currentItemToModifier.GroupNameRu ||
					itemToModifier.GroupNameKz != currentItemToModifier.GroupNameKz ||
					itemToModifier.MaxQuantity != currentItemToModifier.GroupMaxQuantity ||
					itemToModifier.MinQuantity != currentItemToModifier.GroupMinQuantity ||
					itemToModifier.Modifier.MaxQuantity != currentItemToModifier.MaxQuantity ||
					itemToModifier.Modifier.MinQuantity != currentItemToModifier.MinQuantity ||
					itemToModifier.Modifier.DefaultQuantity != currentItemToModifier.DefaultQuantity ||
					itemToModifier.Modifier.Price != currentItemToModifier.PriceOverride {

					currentItemToModifier.GroupNameRu = itemToModifier.GroupName
					currentItemToModifier.GroupNameKz = itemToModifier.GroupNameKz
					currentItemToModifier.GroupMaxQuantity = itemToModifier.MaxQuantity
					currentItemToModifier.GroupMinQuantity = itemToModifier.MinQuantity
					currentItemToModifier.MaxQuantity = itemToModifier.Modifier.MaxQuantity
					currentItemToModifier.MinQuantity = itemToModifier.Modifier.MinQuantity
					currentItemToModifier.DefaultQuantity = itemToModifier.Modifier.DefaultQuantity
					currentItemToModifier.PriceOverride = itemToModifier.Modifier.Price

					m, err := toDB(currentItemToModifier)
					if err != nil {
						return fmt.Errorf("failed to toDB: %w", err)
					}

					delete(m, "farsh_menu_item_dst_id")
					delete(m, "farsh_menu_item_src_id")
					delete(m, "station_type")

					_, err = p.repo.UpsertConfigEntityRelation(ctx, tableItemsToModifiers, "farsh_menu_item_src_id", currentItemToModifier.ItemSrcID, "farsh_menu_item_dst_id", currentItemToModifier.ItemDdcID, m)
					if err != nil {
						return fmt.Errorf("failed to upsert items to modifiers: %w", err)
					}
				}
			} else {
				m, err := toDB(iikoServiceMinuItemToModifier{
					ItemSrcID:               currentItem.Id,
					ItemDdcID:               currentModifier.Id,
					AppliesToSizeExternalID: "",
					ExternalGroupID:         "",
					GroupCode:               "",
					GroupNameRu:             itemToModifier.GroupName,
					GroupMaxQuantity:        itemToModifier.MaxQuantity,
					GroupMinQuantity:        itemToModifier.MinQuantity,
					GroupNameKz:             itemToModifier.GroupNameKz,
					HideIfDefault:           false,
					IncludedFree:            false,
					IsDefault:               false,
					MaxQuantity:             itemToModifier.Modifier.MaxQuantity,
					MinQuantity:             itemToModifier.Modifier.MinQuantity,
					DefaultQuantity:         itemToModifier.Modifier.DefaultQuantity,
					PriceOverride:           itemToModifier.Modifier.Price,
					StationType:             stationType,
				})
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}

				delete(m, "station_type")
				delete(m, "farsh_menu_item_dst_id")
				delete(m, "farsh_menu_item_src_id")

				_, err = p.repo.UpsertConfigEntityRelation(ctx, tableItemsToModifiers, "farsh_menu_item_src_id", currentItem.Id, "farsh_menu_item_dst_id", currentModifier.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert items to modifiers: %w", err)
				}
			}

			delete(currentMap, itemToModifier.Modifier.ID.String())
		}

		for _, current := range currentMap {
			forHide = append(forHide, current)
		}

		delete(currentItemsToModifiersMap, itemsToModifiersIndex.String())
	}

	for _, itemsToModifiers := range currentItemsToModifiersMap {
		for _, current := range itemsToModifiers {
			forHide = append(forHide, current)
		}
	}

	for _, itemToModifier := range forHide {
		err := p.repo.DeleteConfigEntityRowRelation(ctx, tableItemsToModifiers, "farsh_menu_item_src_id", itemToModifier.ItemSrcID, "farsh_menu_item_dst_id", itemToModifier.ItemDdcID)
		if err != nil {
			return fmt.Errorf("failed to delete items to modifiers: %w", err)
		}
	}

	return nil
}

func (p *plugin) syncStopList(ctx context.Context, items []models.IikoServiceMenuItem, modifiers []models.IikoServiceMenuItemModifier) error {
	rawCurrentEntries, err := p.repo.GetConfigTableRows(ctx, tableStopList)
	if err != nil {
		return fmt.Errorf("failed to get stop list: %w", err)
	}
	currentEntries, err := utils.SelectE(rawCurrentEntries, fromDB[iikoServiceStopListEntry])
	if err != nil {
		return fmt.Errorf("failed to select stop list: %w", err)
	}
	currentEntriesMap := utils.Map(currentEntries, func(e iikoServiceStopListEntry) string {
		return e.Key
	})

	maxId := int64(0)
	if len(currentEntries) > 0 {
		maxId = slices.MaxFunc(utils.Select(currentEntries, func(e iikoServiceStopListEntry) int64 { return e.Id }), func(i int64, maximum int64) int {
			return int(i - maximum)
		})
	}

	processEntry := func(productID string, isStopped bool) error {
		key := productID
		if current, ok := currentEntriesMap[key]; ok {
			if current.IsStopped != isStopped {
				current.IsStopped = isStopped
				m, err := toDB(current)
				if err != nil {
					return fmt.Errorf("failed to toDB: %w", err)
				}
				_, err = p.repo.UpsertConfigEntityRow(ctx, tableStopList, current.Id, m)
				if err != nil {
					return fmt.Errorf("failed to upsert stop list entry: %w", err)
				}
			}
			delete(currentEntriesMap, key)
		} else {
			maxId++
			now := time.Now().UTC()
			m, err := toDB(iikoServiceStopListEntry{
				Id:                maxId,
				Key:               key,
				ExternalProductID: productID,
				IsStopped:         isStopped,
				StationType:       stationType,
				CreatedAt:         now,
				UpdatedAt:         now,
			})
			if err != nil {
				return fmt.Errorf("failed to toDB: %w", err)
			}
			_, err = p.repo.UpsertConfigEntityRow(ctx, tableStopList, maxId, m)
			if err != nil {
				return fmt.Errorf("failed to upsert stop list entry: %w", err)
			}
		}
		return nil
	}

	for _, item := range items {
		if err := processEntry(item.ID.String(), item.IsStopped); err != nil {
			return err
		}
	}
	for _, modifier := range modifiers {
		if err := processEntry(modifier.ID.String(), modifier.IsStopped); err != nil {
			return err
		}
	}

	// Update entries for products no longer in menu
	for _, entry := range currentEntriesMap {
		if !entry.IsStopped {
			continue
		}
		entry.IsStopped = false
		m, err := toDB(entry)
		if err != nil {
			return fmt.Errorf("failed to toDB: %w", err)
		}
		_, err = p.repo.UpsertConfigEntityRow(ctx, tableStopList, entry.Id, m)
		if err != nil {
			return fmt.Errorf("failed to upsert stop list entry: %w", err)
		}
	}

	return nil
}

func (p *plugin) fetchImageB64ByURL(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("URL does not point to an image, got Content-Type: %s", contentType)
	}

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read image data: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	img = cropAndResize(img)

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return "", fmt.Errorf("failed to encode image to png: %w", err)
	}

	base64Str := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	return "data:image/png;base64," + base64Str, nil
}

func cropAndResize(img image.Image) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	targetRatio := float64(imageTargetWidth) / float64(imageTargetHeight)
	imgRatio := float64(w) / float64(h)

	cropW, cropH := w, h
	if imgRatio > targetRatio {
		cropW = int(float64(h) * targetRatio)
	} else if imgRatio < targetRatio {
		cropH = int(float64(w) / targetRatio)
	}

	x0 := bounds.Min.X + (w-cropW)/2
	y0 := bounds.Min.Y + (h-cropH)/2

	cropRect := image.Rect(0, 0, cropW, cropH)
	cropped := image.NewRGBA(cropRect)
	draw.Draw(cropped, cropRect, img, image.Pt(x0, y0), draw.Src)

	if cropW <= imageTargetWidth && cropH <= imageTargetHeight {
		return cropped
	}

	dstRect := image.Rect(0, 0, imageTargetWidth, imageTargetHeight)
	dst := image.NewRGBA(dstRect)
	draw.CatmullRom.Scale(dst, dstRect, cropped, cropRect, draw.Src, nil)

	return dst
}

func parseOrderItem(key string, value int) (orderItem, error) {
	keyParts := strings.Split(key, "||")
	if len(keyParts) != 4 {
		return orderItem{}, fmt.Errorf("invalid key: %s", key)
	}

	item := orderItem{
		ID:       keyParts[0],
		Quantity: value,
	}

	if keyParts[1] != "s=none" {
		sizeParts := strings.Split(keyParts[1], "=")
		if len(sizeParts) != 2 {
			return orderItem{}, fmt.Errorf("invalid size: %s", keyParts[1])
		}

		item.SizeId = &sizeParts[1]
	}

	if keyParts[2] != "d=none" {
		dressingParts := strings.Split(keyParts[2], "=")
		if len(dressingParts) != 2 {
			return orderItem{}, fmt.Errorf("invalid dressing: %s", keyParts[2])
		}

		item.Modifiers = append(item.Modifiers, dressingParts[1])
	}

	if keyParts[3] != "m=none" {
		modifiersParts := strings.Split(keyParts[3], "=")
		if len(modifiersParts) != 2 {
			return orderItem{}, fmt.Errorf("invalid modifiers: %s", keyParts[3])
		}

		item.Modifiers = append(item.Modifiers, strings.Split(modifiersParts[1], "+")...)
	}

	return item, nil
}
