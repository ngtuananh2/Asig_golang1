# Task Management API

REST API quản lý người dùng, dự án, công việc và bình luận. Stack: Go, Gin, GORM, PostgreSQL, Redis, JWT, bcrypt và Docker.

## Chạy local

1. Sao chép `.env.example` thành `.env` và thay `JWT_SECRET` bằng chuỗi bí mật dài, ngẫu nhiên.
2. Chạy `docker compose up --build`.
3. API chạy tại `http://localhost:8080`; kiểm tra `GET /health`.

Compose khởi động PostgreSQL và Redis, đợi health check rồi mới khởi động API. GORM tạo/cập nhật schema khi ứng dụng khởi động. Có thể chạy không Docker bằng cách đặt `DATABASE_URL`, `JWT_SECRET`, `REDIS_URL` rồi chạy `go run ./cmd/api`.

### Kiến trúc

```text
Client
  -> Gin middleware (logging, recovery, CORS, rate limit)
  -> JWT middleware (các route riêng tư)
  -> Handler (HTTP, binding/validation, JSON response)
  -> Service (đăng ký, xác thực mật khẩu)
  -> Repository / GORM
  -> PostgreSQL
Handler <-> Redis (cache GET theo user, TTL 30 giây)
```

Mã khởi động nằm ở `cmd/api`; các thành phần ứng dụng nằm trong `internal/handlers`, `internal/services`, `internal/repositories`, `internal/models` và `internal/middlewares`. Schema được GORM `AutoMigrate` khi ứng dụng khởi động. CRUD hiện truy cập GORM trực tiếp từ handler; repository/service đang được dùng chủ yếu cho luồng đăng ký và xác thực.

## Authentication và quyền

Đăng ký tại `POST /api/v1/auth/register`, đăng nhập tại `POST /api/v1/auth/login`. Gửi token ở header `Authorization: Bearer <token>`. User chỉ đọc/sửa/xóa dữ liệu của mình. Admin được phép xem/quản lý user khác; admin có thể tạo user mới qua `POST /api/v1/users`.

Để gán admin đầu tiên cho tài khoản đã đăng ký trong môi trường local:

```sh
docker compose exec db psql -U postgres -d taskapi -c "UPDATE users SET role='admin' WHERE email='you@example.com';"
```

Chỉ người vận hành database mới nên chạy câu lệnh này. API không cho user tự đổi role.

## Endpoints

| Resource | Endpoints |
|---|---|
| Auth | `POST /api/v1/auth/register`, `POST /api/v1/auth/login` |
| Users | `GET/POST /api/v1/users`, `GET/PUT/DELETE /api/v1/users/:id` |
| Projects | `GET/POST /api/v1/projects`, `GET/PUT/DELETE /api/v1/projects/:id` |
| Tasks | `GET/POST /api/v1/tasks`, `GET/PUT/DELETE /api/v1/tasks/:id` |
| Comments | `GET/POST /api/v1/comments`, `GET/PUT/DELETE /api/v1/comments/:id` |

Các route resource cần JWT. GET list hỗ trợ `page`, `limit` (tối đa 100), `sort`, `order`; thêm `q` cho project/user, `status` và `project_id` cho task, `task_id` cho comment. Sort field được whitelist. Comment và task chỉ tham chiếu resource thuộc user hiện tại.

Success trả `{"data": ...}`; list kèm `meta`. Error trả `{"error":{"code":"...","message":"..."}}`. Tài liệu OpenAPI đầy đủ ở [docs/openapi.yaml](docs/openapi.yaml).

### Thử API bằng curl

Các ví dụ dưới đây dùng Bash/Git Bash. Sau khi service khỏe, đăng ký user và đăng nhập để nhận JWT:

```sh
curl -sS http://localhost:8080/health
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"name":"Ada Lovelace","email":"ada@example.com","password":"correct-horse"}'
curl -sS -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"ada@example.com","password":"correct-horse"}'
```

Lấy trường `data.token` từ response đăng nhập, đặt vào biến `TOKEN`, rồi gọi route private:

```sh
export TOKEN="<JWT nhận được từ bước login>"
curl -sS http://localhost:8080/api/v1/projects \
  -H "Authorization: Bearer $TOKEN"
curl -sS -X POST http://localhost:8080/api/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Research roadmap","description":"Plan the next sprint"}'
curl -sS "http://localhost:8080/api/v1/projects?page=1&limit=10&sort=name&order=asc" \
  -H "Authorization: Bearer $TOKEN"
```

`/` không phải trang web và sẽ trả JSON 404; dùng `/health` để kiểm tra server. Nếu container API thoát, xem nguyên nhân bằng `docker compose ps -a` và `docker compose logs api`. Đảm bảo `JWT_SECRET` trong `.env` dài ít nhất 16 ký tự; không chia sẻ hoặc commit `.env`.

## Redis, logging, CORS và rate limit

GET của resource private được cache theo user và query trong 30 giây. POST/PUT/DELETE xóa cache của user đó. Khi Redis không khả dụng, API tiếp tục chạy không cache. Rate limit hiện là 120 request/phút trên mỗi IP trong từng process; nếu chạy nhiều replica cần chuyển limiter sang Redis để chia sẻ quota. Gin access logger ghi method, đường dẫn, status và thời gian; recovery và lỗi route trả JSON.

## Kiểm tra

```sh
go test ./...
go vet ./...
go build ./...
```

CI chạy gofmt check, vet, unit test, build và golangci-lint ở mỗi push/PR. Test service kiểm tra hash bcrypt, email trùng và xác thực mật khẩu; middleware có test JWT và rate limit.

## Deploy lên Render

`render.yaml` định nghĩa web service Docker, PostgreSQL và Key Value Redis. Đẩy repository lên GitHub, tạo Blueprint trong Render trỏ tới repository và xác nhận tạo các service. Render tạo `JWT_SECRET` ngẫu nhiên, truyền connection string nội bộ và kiểm tra `/health`. Sau deploy, đăng ký tài khoản, kết nối database qua dashboard để cấp role admin như hướng dẫn ở trên, rồi kiểm tra các route bằng Postman hoặc curl. Điền URL thật vào báo cáo sau khi deploy.

Free Postgres trên Render hết hạn sau 30 ngày và free Key Value không lưu dữ liệu qua lần restart; chọn plan có lưu trữ bền nếu cần giữ dữ liệu. API có thể deploy bằng Docker trên VPS hoặc các nền tảng container khác bằng cách cung cấp ba biến `DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`.

## Báo cáo

Bản Word hiện tại: [docs/BAO_CAO_BAI_TAP_GO.docx](docs/BAO_CAO_BAI_TAP_GO.docx). Bản Markdown tham khảo: [docs/report.md](docs/report.md). Điền mã học viên, URL deploy và video demo trước khi nộp. Video Postman/curl cần quay thực tế tối thiểu 2 phút.
