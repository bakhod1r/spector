package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/specter"
	"github.com/user/specter/mount"
)

type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type Address struct {
	Line1   string `json:"line1"`
	Line2   string `json:"line2,omitempty"`
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Zip     string `json:"zip"`
}

type Audit struct {
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	CreatedBy string    `json:"createdBy"`
}

type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type Contact struct {
	Email string `json:"email"`
	Phone string `json:"phone,omitempty"`
}

type LineItem struct {
	ProductID int   `json:"productId"`
	Quantity  int   `json:"quantity"`
	Price     Money `json:"price"`
}

type User struct {
	ID       int               `json:"id"`
	Name     string            `json:"name"`
	Email    string            `json:"email"`
	Roles    []string          `json:"roles"`
	Contact  Contact           `json:"contact"`
	Billing  Address           `json:"billing"`
	Active   bool              `json:"active"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Audit    Audit             `json:"audit"`
}

type CreateUserReq struct {
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

type Product struct {
	ID          int               `json:"id"`
	Sku         string            `json:"sku"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Price       Money             `json:"price"`
	Tags        []string          `json:"tags"`
	Attributes  map[string]string `json:"attributes"`
	Image       string            `json:"image" doc:"URL of the product photo"`
	InStock     bool              `json:"inStock"`
	Rating      float64           `json:"rating"`
	Audit       Audit             `json:"audit"`
}

type CreateProductReq struct {
	Image string   `json:"image"`
	Sku   string   `json:"sku"`
	Title string   `json:"title"`
	Price Money    `json:"price"`
	Tags  []string `json:"tags"`
}

type Order struct {
	ID     int        `json:"id"`
	UserID int        `json:"userId"`
	Items  []LineItem `json:"items"`
	Total  Money      `json:"total"`
	Status string     `json:"status"`
	ShipTo Address    `json:"shipTo"`
	Placed time.Time  `json:"placed"`
	Audit  Audit      `json:"audit"`
}

type CreateOrderReq struct {
	UserID int        `json:"userId"`
	Items  []LineItem `json:"items"`
	ShipTo Address    `json:"shipTo"`
}

type Cart struct {
	ID       int        `json:"id"`
	UserID   int        `json:"userId"`
	Items    []LineItem `json:"items"`
	Subtotal Money      `json:"subtotal"`
	Audit    Audit      `json:"audit"`
}

type CreateCartReq struct {
	UserID int        `json:"userId" binding:"required,gte=1"`
	Email  string     `json:"email" binding:"required,email"`
	Note   string     `json:"note" binding:"max=200"`
	Tier   string     `json:"tier" binding:"oneof=free pro enterprise"`
	Items  []LineItem `json:"items" binding:"required,min=1,max=50"`
}

type Payment struct {
	ID       int    `json:"id"`
	OrderID  int    `json:"orderId"`
	Amount   Money  `json:"amount"`
	Method   string `json:"method"`
	Status   string `json:"status"`
	Captured bool   `json:"captured"`
	Audit    Audit  `json:"audit"`
}

type CreatePaymentReq struct {
	OrderID int    `json:"orderId"`
	Amount  Money  `json:"amount"`
	Method  string `json:"method"`
}

type Invoice struct {
	ID      int        `json:"id"`
	OrderID int        `json:"orderId"`
	Number  string     `json:"number"`
	Lines   []LineItem `json:"lines"`
	Total   Money      `json:"total"`
	Paid    bool       `json:"paid"`
	Due     time.Time  `json:"due"`
}

type CreateInvoiceReq struct {
	OrderID int    `json:"orderId"`
	Number  string `json:"number"`
}

type Subscription struct {
	ID     int       `json:"id"`
	UserID int       `json:"userId"`
	Plan   string    `json:"plan"`
	Price  Money     `json:"price"`
	Renews time.Time `json:"renews"`
	Active bool      `json:"active"`
	Audit  Audit     `json:"audit"`
}

type CreateSubscriptionReq struct {
	UserID int    `json:"userId"`
	Plan   string `json:"plan"`
}

type Shipment struct {
	ID          int      `json:"id"`
	OrderID     int      `json:"orderId"`
	Carrier     string   `json:"carrier"`
	Tracking    string   `json:"tracking"`
	Origin      GeoPoint `json:"origin"`
	Destination GeoPoint `json:"destination"`
	Delivered   bool     `json:"delivered"`
}

type CreateShipmentReq struct {
	OrderID int    `json:"orderId"`
	Carrier string `json:"carrier"`
}

type Review struct {
	ID        int    `json:"id"`
	ProductID int    `json:"productId"`
	UserID    int    `json:"userId"`
	Stars     int    `json:"stars"`
	Body      string `json:"body"`
	Verified  bool   `json:"verified"`
	Audit     Audit  `json:"audit"`
}

type CreateReviewReq struct {
	ProductID int    `json:"productId"`
	Stars     int    `json:"stars"`
	Body      string `json:"body"`
}

type Category struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Slug     string   `json:"slug"`
	ParentID int      `json:"parentId,omitempty"`
	Path     []string `json:"path"`
}

type CreateCategoryReq struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Coupon struct {
	ID      int       `json:"id"`
	Code    string    `json:"code"`
	Percent float64   `json:"percent"`
	Expires time.Time `json:"expires"`
	Active  bool      `json:"active"`
}

type CreateCouponReq struct {
	Code    string  `json:"code"`
	Percent float64 `json:"percent"`
}

type Warehouse struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Location Address  `json:"location"`
	Geo      GeoPoint `json:"geo"`
	Capacity int      `json:"capacity"`
}

type CreateWarehouseReq struct {
	Name     string  `json:"name"`
	Location Address `json:"location"`
}

type Inventory struct {
	ID          int `json:"id"`
	ProductID   int `json:"productId"`
	WarehouseID int `json:"warehouseId"`
	Quantity    int `json:"quantity"`
	Reserved    int `json:"reserved"`
}

type CreateInventoryReq struct {
	ProductID   int `json:"productId"`
	WarehouseID int `json:"warehouseId"`
	Quantity    int `json:"quantity"`
}

type Supplier struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Contact Contact `json:"contact"`
	Address Address `json:"address"`
	Rating  float64 `json:"rating"`
}

type CreateSupplierReq struct {
	Name    string  `json:"name"`
	Contact Contact `json:"contact"`
}

type Notification struct {
	ID     int       `json:"id"`
	UserID int       `json:"userId"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Read   bool      `json:"read"`
	Sent   time.Time `json:"sent"`
}

type CreateNotificationReq struct {
	UserID int    `json:"userId"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
}

type Ticket struct {
	ID       int      `json:"id"`
	UserID   int      `json:"userId"`
	Subject  string   `json:"subject"`
	Priority string   `json:"priority"`
	Status   string   `json:"status"`
	Tags     []string `json:"tags"`
	Audit    Audit    `json:"audit"`
}

type CreateTicketReq struct {
	UserID   int    `json:"userId"`
	Subject  string `json:"subject"`
	Priority string `json:"priority"`
}

type Message struct {
	ID       int       `json:"id"`
	TicketID int       `json:"ticketId"`
	From     string    `json:"from"`
	Body     string    `json:"body"`
	Sent     time.Time `json:"sent"`
}

type CreateMessageReq struct {
	TicketID int    `json:"ticketId"`
	Body     string `json:"body"`
}

type Team struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Members []int  `json:"members"`
	Owner   int    `json:"owner"`
	Audit   Audit  `json:"audit"`
}

type CreateTeamReq struct {
	Name  string `json:"name"`
	Owner int    `json:"owner"`
}

type Project struct {
	ID       int       `json:"id"`
	TeamID   int       `json:"teamId"`
	Name     string    `json:"name"`
	Budget   Money     `json:"budget"`
	Deadline time.Time `json:"deadline"`
	Tags     []string  `json:"tags"`
	Audit    Audit     `json:"audit"`
}

type CreateProjectReq struct {
	TeamID int    `json:"teamId"`
	Name   string `json:"name"`
	Budget Money  `json:"budget"`
}

type Task struct {
	ID        int               `json:"id"`
	ProjectID int               `json:"projectId"`
	Title     string            `json:"title"`
	Assignee  int               `json:"assignee"`
	Done      bool              `json:"done"`
	Labels    []string          `json:"labels"`
	Meta      map[string]string `json:"meta,omitempty"`
}

type CreateTaskReq struct {
	ProjectID int    `json:"projectId"`
	Title     string `json:"title"`
	Assignee  int    `json:"assignee"`
}

func listUsers(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out, err := db.listUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func getUser(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := db.getUser(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, m)
}

func createUser(c *gin.Context) {
	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := db.createUser(User{
		Name: req.Name, Email: req.Email, Roles: req.Roles,
		Contact: Contact{Email: req.Email}, Active: true,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, out)
}

func updateUser(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := db.getUser(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	// An update carries only the fields the create request models, so the rest
	// of the record is preserved rather than blanked.
	existing.Name, existing.Email = req.Name, req.Email
	if req.Roles != nil {
		existing.Roles = req.Roles
	}
	out, err := db.updateUser(id, existing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func deleteUser(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := db.getUser(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if err := db.deleteUser(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func listProducts(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out, err := db.listProducts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func getProduct(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := db.getProduct(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	c.JSON(http.StatusOK, m)
}

func createProduct(c *gin.Context) {
	var req CreateProductReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := db.createProduct(Product{
		Sku: req.Sku, Title: req.Title, Price: req.Price, Tags: req.Tags, Image: req.Image, InStock: true,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, out)
}

func updateProduct(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req CreateProductReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := db.getProduct(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	existing.Sku, existing.Title, existing.Price = req.Sku, req.Title, req.Price
	existing.Image = req.Image
	if req.Tags != nil {
		existing.Tags = req.Tags
	}
	out, err := db.updateProduct(id, existing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func deleteProduct(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := db.getProduct(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	if err := db.deleteProduct(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func listOrders(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out, err := db.listOrders()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func getOrder(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := db.getOrder(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, m)
}

func createOrder(c *gin.Context) {
	var req CreateOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := db.createOrder(Order{UserID: req.UserID, Items: req.Items, ShipTo: req.ShipTo})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, out)
}

func updateOrder(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req CreateOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := db.getOrder(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	existing.UserID, existing.Items, existing.ShipTo = req.UserID, req.Items, req.ShipTo
	out, err := db.updateOrder(id, existing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func deleteOrder(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := db.getOrder(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if err := db.deleteOrder(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

// listCarts returns every cart.
// specter:tags carts
func listCarts(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Cart{}
	c.JSON(http.StatusOK, out)
}

func getCart(c *gin.Context) {
	var m Cart
	c.JSON(http.StatusOK, m)
}

// createCart opens a new cart.
// specter:tags carts,write
// specter:operationId openCart
func createCart(c *gin.Context) {
	var req CreateCartReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Cart{})
}

func updateCart(c *gin.Context) {
	var req CreateCartReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Cart{})
}

func deleteCart(c *gin.Context) {
	var m Cart
	c.JSON(http.StatusOK, m)
}

func listPayments(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Payment{}
	c.JSON(http.StatusOK, out)
}

func getPayment(c *gin.Context) {
	var m Payment
	c.JSON(http.StatusOK, m)
}

func createPayment(c *gin.Context) {
	var req CreatePaymentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Payment{})
}

func updatePayment(c *gin.Context) {
	var req CreatePaymentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Payment{})
}

func deletePayment(c *gin.Context) {
	var m Payment
	c.JSON(http.StatusOK, m)
}

func listInvoices(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Invoice{}
	c.JSON(http.StatusOK, out)
}

func getInvoice(c *gin.Context) {
	var m Invoice
	c.JSON(http.StatusOK, m)
}

func createInvoice(c *gin.Context) {
	var req CreateInvoiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Invoice{})
}

func updateInvoice(c *gin.Context) {
	var req CreateInvoiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Invoice{})
}

func deleteInvoice(c *gin.Context) {
	var m Invoice
	c.JSON(http.StatusOK, m)
}

func listSubscriptions(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Subscription{}
	c.JSON(http.StatusOK, out)
}

func getSubscription(c *gin.Context) {
	var m Subscription
	c.JSON(http.StatusOK, m)
}

func createSubscription(c *gin.Context) {
	var req CreateSubscriptionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Subscription{})
}

func updateSubscription(c *gin.Context) {
	var req CreateSubscriptionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Subscription{})
}

func deleteSubscription(c *gin.Context) {
	var m Subscription
	c.JSON(http.StatusOK, m)
}

func listShipments(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Shipment{}
	c.JSON(http.StatusOK, out)
}

func getShipment(c *gin.Context) {
	var m Shipment
	c.JSON(http.StatusOK, m)
}

func createShipment(c *gin.Context) {
	var req CreateShipmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Shipment{})
}

func updateShipment(c *gin.Context) {
	var req CreateShipmentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Shipment{})
}

func deleteShipment(c *gin.Context) {
	var m Shipment
	c.JSON(http.StatusOK, m)
}

func listReviews(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Review{}
	c.JSON(http.StatusOK, out)
}

func getReview(c *gin.Context) {
	var m Review
	c.JSON(http.StatusOK, m)
}

func createReview(c *gin.Context) {
	var req CreateReviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Review{})
}

func updateReview(c *gin.Context) {
	var req CreateReviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Review{})
}

func deleteReview(c *gin.Context) {
	var m Review
	c.JSON(http.StatusOK, m)
}

func listCategorys(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Category{}
	c.JSON(http.StatusOK, out)
}

func getCategory(c *gin.Context) {
	var m Category
	c.JSON(http.StatusOK, m)
}

func createCategory(c *gin.Context) {
	var req CreateCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Category{})
}

func updateCategory(c *gin.Context) {
	var req CreateCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Category{})
}

func deleteCategory(c *gin.Context) {
	var m Category
	c.JSON(http.StatusOK, m)
}

func listCoupons(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Coupon{}
	c.JSON(http.StatusOK, out)
}

func getCoupon(c *gin.Context) {
	var m Coupon
	c.JSON(http.StatusOK, m)
}

func createCoupon(c *gin.Context) {
	var req CreateCouponReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Coupon{})
}

func updateCoupon(c *gin.Context) {
	var req CreateCouponReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Coupon{})
}

func deleteCoupon(c *gin.Context) {
	var m Coupon
	c.JSON(http.StatusOK, m)
}

func listWarehouses(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Warehouse{}
	c.JSON(http.StatusOK, out)
}

func getWarehouse(c *gin.Context) {
	var m Warehouse
	c.JSON(http.StatusOK, m)
}

func createWarehouse(c *gin.Context) {
	var req CreateWarehouseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Warehouse{})
}

func updateWarehouse(c *gin.Context) {
	var req CreateWarehouseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Warehouse{})
}

func deleteWarehouse(c *gin.Context) {
	var m Warehouse
	c.JSON(http.StatusOK, m)
}

func listInventorys(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Inventory{}
	c.JSON(http.StatusOK, out)
}

func getInventory(c *gin.Context) {
	var m Inventory
	c.JSON(http.StatusOK, m)
}

func createInventory(c *gin.Context) {
	var req CreateInventoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Inventory{})
}

func updateInventory(c *gin.Context) {
	var req CreateInventoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Inventory{})
}

func deleteInventory(c *gin.Context) {
	var m Inventory
	c.JSON(http.StatusOK, m)
}

func listSuppliers(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Supplier{}
	c.JSON(http.StatusOK, out)
}

func getSupplier(c *gin.Context) {
	var m Supplier
	c.JSON(http.StatusOK, m)
}

func createSupplier(c *gin.Context) {
	var req CreateSupplierReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Supplier{})
}

func updateSupplier(c *gin.Context) {
	var req CreateSupplierReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Supplier{})
}

func deleteSupplier(c *gin.Context) {
	var m Supplier
	c.JSON(http.StatusOK, m)
}

func listNotifications(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Notification{}
	c.JSON(http.StatusOK, out)
}

func getNotification(c *gin.Context) {
	var m Notification
	c.JSON(http.StatusOK, m)
}

func createNotification(c *gin.Context) {
	var req CreateNotificationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Notification{})
}

func updateNotification(c *gin.Context) {
	var req CreateNotificationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Notification{})
}

func deleteNotification(c *gin.Context) {
	var m Notification
	c.JSON(http.StatusOK, m)
}

func listTickets(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Ticket{}
	c.JSON(http.StatusOK, out)
}

func getTicket(c *gin.Context) {
	var m Ticket
	c.JSON(http.StatusOK, m)
}

func createTicket(c *gin.Context) {
	var req CreateTicketReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Ticket{})
}

func updateTicket(c *gin.Context) {
	var req CreateTicketReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Ticket{})
}

func deleteTicket(c *gin.Context) {
	var m Ticket
	c.JSON(http.StatusOK, m)
}

func listMessages(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Message{}
	c.JSON(http.StatusOK, out)
}

func getMessage(c *gin.Context) {
	var m Message
	c.JSON(http.StatusOK, m)
}

func createMessage(c *gin.Context) {
	var req CreateMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Message{})
}

func updateMessage(c *gin.Context) {
	var req CreateMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Message{})
}

func deleteMessage(c *gin.Context) {
	var m Message
	c.JSON(http.StatusOK, m)
}

func listTeams(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Team{}
	c.JSON(http.StatusOK, out)
}

func getTeam(c *gin.Context) {
	var m Team
	c.JSON(http.StatusOK, m)
}

func createTeam(c *gin.Context) {
	var req CreateTeamReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Team{})
}

func updateTeam(c *gin.Context) {
	var req CreateTeamReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Team{})
}

func deleteTeam(c *gin.Context) {
	var m Team
	c.JSON(http.StatusOK, m)
}

func listProjects(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Project{}
	c.JSON(http.StatusOK, out)
}

func getProject(c *gin.Context) {
	var m Project
	c.JSON(http.StatusOK, m)
}

func createProject(c *gin.Context) {
	var req CreateProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Project{})
}

func updateProject(c *gin.Context) {
	var req CreateProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Project{})
}

func deleteProject(c *gin.Context) {
	var m Project
	c.JSON(http.StatusOK, m)
}

func listTasks(c *gin.Context) {
	q := c.Query("q")
	status := c.DefaultQuery("status", "")
	limit := c.DefaultQuery("limit", "20")
	sort := c.Query("sort")
	_, _, _, _ = q, status, limit, sort
	out := []Task{}
	c.JSON(http.StatusOK, out)
}

func getTask(c *gin.Context) {
	var m Task
	c.JSON(http.StatusOK, m)
}

func createTask(c *gin.Context) {
	var req CreateTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, Task{})
}

func updateTask(c *gin.Context) {
	var req CreateTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, Task{})
}

func deleteTask(c *gin.Context) {
	var m Task
	c.JSON(http.StatusOK, m)
}

// pathID reads the :id path parameter as an integer.
func pathID(c *gin.Context) (int, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return 0, fmt.Errorf("id must be a number, got %q", c.Param("id"))
	}
	return id, nil
}

// cfg holds the configuration read at startup, so router() can reach the
// console settings without re-reading the environment.
var cfg Config

func main() {
	cfg = loadConfig()

	var err error
	if db, err = openStore(cfg.DB); err != nil {
		log.Fatalf("shop: opening %s: %v", cfg.DB, err)
	}
	log.Printf("shop: using %s", cfg.DB)

	r := router()
	go startGRPC(cfg.GRPCAddr)
	r.Run(fmt.Sprintf(":%d", cfg.Port))
}

// router builds the engine and registers every route. It is separate from
// main so tests can drive the API without binding a port.
func router() *gin.Engine {
	r := gin.Default()

	v1 := r.Group("/api/v1")
	v1.GET("/users", listUsers)
	v1.GET("/users/:id", getUser)
	v1.POST("/users", createUser)
	v1.PUT("/users/:id", updateUser)
	v1.DELETE("/users/:id", deleteUser)
	v1.GET("/products", listProducts)
	v1.GET("/products/:id", getProduct)
	v1.POST("/products", createProduct)
	v1.PUT("/products/:id", updateProduct)
	v1.DELETE("/products/:id", deleteProduct)
	v1.GET("/orders", listOrders)
	v1.GET("/orders/:id", getOrder)
	v1.POST("/orders", createOrder)
	v1.PUT("/orders/:id", updateOrder)
	v1.DELETE("/orders/:id", deleteOrder)
	v1.GET("/carts", listCarts)
	v1.GET("/carts/:id", getCart)
	v1.POST("/carts", createCart)
	v1.PUT("/carts/:id", updateCart)
	v1.DELETE("/carts/:id", deleteCart)
	v1.GET("/payments", listPayments)
	v1.GET("/payments/:id", getPayment)
	v1.POST("/payments", createPayment)
	v1.PUT("/payments/:id", updatePayment)
	v1.DELETE("/payments/:id", deletePayment)
	v1.GET("/invoices", listInvoices)
	v1.GET("/invoices/:id", getInvoice)
	v1.POST("/invoices", createInvoice)
	v1.PUT("/invoices/:id", updateInvoice)
	v1.DELETE("/invoices/:id", deleteInvoice)
	v1.GET("/subscriptions", listSubscriptions)
	v1.GET("/subscriptions/:id", getSubscription)
	v1.POST("/subscriptions", createSubscription)
	v1.PUT("/subscriptions/:id", updateSubscription)
	v1.DELETE("/subscriptions/:id", deleteSubscription)
	v1.GET("/shipments", listShipments)
	v1.GET("/shipments/:id", getShipment)
	v1.POST("/shipments", createShipment)
	v1.PUT("/shipments/:id", updateShipment)
	v1.DELETE("/shipments/:id", deleteShipment)
	v1.GET("/reviews", listReviews)
	v1.GET("/reviews/:id", getReview)
	v1.POST("/reviews", createReview)
	v1.PUT("/reviews/:id", updateReview)
	v1.DELETE("/reviews/:id", deleteReview)
	v1.GET("/categories", listCategorys)
	v1.GET("/categories/:id", getCategory)
	v1.POST("/categories", createCategory)
	v1.PUT("/categories/:id", updateCategory)
	v1.DELETE("/categories/:id", deleteCategory)
	v1.GET("/coupons", listCoupons)
	v1.GET("/coupons/:id", getCoupon)
	v1.POST("/coupons", createCoupon)
	v1.PUT("/coupons/:id", updateCoupon)
	v1.DELETE("/coupons/:id", deleteCoupon)
	v1.GET("/warehouses", listWarehouses)
	v1.GET("/warehouses/:id", getWarehouse)
	v1.POST("/warehouses", createWarehouse)
	v1.PUT("/warehouses/:id", updateWarehouse)
	v1.DELETE("/warehouses/:id", deleteWarehouse)
	v1.GET("/inventory", listInventorys)
	v1.GET("/inventory/:id", getInventory)
	v1.POST("/inventory", createInventory)
	v1.PUT("/inventory/:id", updateInventory)
	v1.DELETE("/inventory/:id", deleteInventory)
	v1.GET("/suppliers", listSuppliers)
	v1.GET("/suppliers/:id", getSupplier)
	v1.POST("/suppliers", createSupplier)
	v1.PUT("/suppliers/:id", updateSupplier)
	v1.DELETE("/suppliers/:id", deleteSupplier)
	v1.GET("/notifications", listNotifications)
	v1.GET("/notifications/:id", getNotification)
	v1.POST("/notifications", createNotification)
	v1.PUT("/notifications/:id", updateNotification)
	v1.DELETE("/notifications/:id", deleteNotification)
	v1.GET("/tickets", listTickets)
	v1.GET("/tickets/:id", getTicket)
	v1.POST("/tickets", createTicket)
	v1.PUT("/tickets/:id", updateTicket)
	v1.DELETE("/tickets/:id", deleteTicket)
	v1.GET("/messages", listMessages)
	v1.GET("/messages/:id", getMessage)
	v1.POST("/messages", createMessage)
	v1.PUT("/messages/:id", updateMessage)
	v1.DELETE("/messages/:id", deleteMessage)
	v1.GET("/teams", listTeams)
	v1.GET("/teams/:id", getTeam)
	v1.POST("/teams", createTeam)
	v1.PUT("/teams/:id", updateTeam)
	v1.DELETE("/teams/:id", deleteTeam)
	v1.GET("/projects", listProjects)
	v1.GET("/projects/:id", getProject)
	v1.POST("/projects", createProject)
	v1.PUT("/projects/:id", updateProject)
	v1.DELETE("/projects/:id", deleteProject)
	v1.GET("/tasks", listTasks)
	v1.GET("/tasks/:id", getTask)
	v1.POST("/tasks", createTask)
	v1.PUT("/tasks/:id", updateTask)
	v1.DELETE("/tasks/:id", deleteTask)

	// Realtime endpoints for the console's Realtime tab.
	r.GET("/events", sseHandler)
	r.GET("/ws", wsHandler)

	// A real GraphQL endpoint so the console's GraphQL tab can execute, not
	// just document.
	if schema, err := newGraphqlSchema(); err == nil {
		r.POST("/graphql", graphqlHandler(schema))
	}

	mount.Gin(r, specter.Config{
		Dir:        ".",
		ProtoDir:   "proto",
		GraphqlDir: "graphql",
		Title:      "Shop API",
		Version:    "2.0.0",
		// Unset by default so the example stays easy to open. Set SPECTER_KEY
		// to see the gate: /docs/ then 404s until you pass ?key=<value>.
		// Both default when unset: the console lives at /docs and is open.
		BasePath:  cfg.BasePath,
		AccessKey: cfg.AccessKey,
		// Adds the "Admin panel" button to the console. The panel runs as its
		// own process (examples/shop/admin), so this is only a link.
		AdminURL: cfg.AdminURL,

		// Neither can be read from source, so they are declared here.
		Servers: []specter.Server{
			{URL: "http://localhost:8080", Description: "local"},
		},
		Security: map[string]specter.SecurityScheme{
			"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
		},
	})

	return r
}
