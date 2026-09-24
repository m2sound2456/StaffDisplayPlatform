# Staff Display Platform --- BLUEPRINT v1.2

> เป้าหมาย: สร้างระบบ Web/PWA สำหรับแสดงโปรไฟล์พนักงานและเนื้อหาหน้าร้านบน Tablet
> โดยรองรับหลายร้านค้า (Multi-tenant) จากระบบเดียว
>
> เอกสารนี้เป็น Source of Truth สำหรับ Cline ในการพัฒนาโปรเจกต์

------------------------------------------------------------------------

## 1. Project Vision

ระบบชื่อชั่วคราว: **Staff Display Platform**

เป็นระบบ SaaS สำหรับร้านค้าที่มี Tablet ตั้งบริเวณหน้าร้าน
เพื่อให้ลูกค้าที่เดินผ่านสามารถเห็น:

-   รูปพนักงานที่อยู่ในร้าน
-   ชื่อ / ชื่อเล่น
-   สถานะ เช่น Available / Busy / Break
-   Promotion
-   QR Code
-   ข้อมูลร้าน
-   เนื้อหาอื่นที่เจ้าของร้านกำหนด

ระบบต้องรองรับหลายร้านจาก Backend และ Database ชุดเดียว

### หลักการสำคัญ

1.  **ไม่ใช้ ESP32**
2.  Tablet ทำงานผ่าน Web/PWA เท่านั้น
3.  Backend และ Database เป็นส่วนกลาง
4.  รองรับ Multi-tenant ตั้งแต่วันแรก
5.  ร้านหนึ่งมีหลาย Tablet ได้
6.  Tablet แต่ละเครื่องต้อง Pair กับร้านและมี Device identity
7.  Admin สามารถจัดการข้อมูลจากมือถือ/PC
8.  Display ต้องทำงานแบบ Full Screen/Kiosk ได้
9.  การเปลี่ยนข้อมูลจาก Admin ควรสะท้อนที่ Display แบบ Realtime
10. ต้องออกแบบให้ขยายจำนวนร้านได้โดยไม่ต้องแยก Server ต่อร้าน

------------------------------------------------------------------------

# 2. Recommended Architecture

``` text
                        Internet
                           |
                           v
                 +-------------------+
                 | Reverse Proxy     |
                 | Nginx / HTTPS     |
                 +---------+---------+
                           |
             +-------------+-------------+
             |                           |
             v                           v
      app.example.com              api.example.com
             |                           |
             v                           v
       React/Vite PWA             Go Backend API
                                         |
                           +-------------+-------------+
                           |                           |
                           v                           v
                    PostgreSQL                   Realtime
                                                   WebSocket
```

Client:

``` text
+--------------------------+
| React / Vite / PWA       |
|                          |
| Admin                    |
| Display                  |
| Device Setup             |
+--------------------------+
```

Backend:

``` text
+--------------------------+
| Go API                   |
|                          |
| Auth                     |
| Tenant                   |
| Store                    |
| Employee                 |
| Device                   |
| Display                  |
| Content                  |
| Realtime                 |
+--------------------------+
```

Database:

``` text
PostgreSQL
```

------------------------------------------------------------------------

# 3. Multi-Tenant Strategy

ใช้ Database เดียวและ Application เดียว

ทุกข้อมูลทางธุรกิจต้องผูกกับ `tenant_id` หรือ `store_id` ตามขอบเขตที่ออกแบบ

ตัวอย่าง:

``` text
Tenant
  |
  +-- Store
       |
       +-- Employees
       +-- Devices
       +-- Display Settings
       +-- Content
```

สำหรับ MVP ให้ถือว่า 1 Tenant = 1 ร้านค้า เพื่อให้โครงสร้างไม่ซับซ้อนเกินไป แต่
Database ควรเปิดทางให้ Tenant หนึ่งมีหลาย Store ได้ในอนาคต

------------------------------------------------------------------------

# 4. Domain & URL Strategy

For MVP and initial production, use **one domain with path-based routing**.

Examples:

```text
https://display.example.com/app
https://display.example.com/s/abc
https://display.example.com/s/coffee
https://display.example.com/setup
https://display.example.com/api
```

## Store URL

Each Store has a unique `slug`.

Example:

```text
Store ABC
slug = abc
```

Its public display route is:

```text
/s/abc
```

Creating a Store in Admin automatically makes this route available. Do NOT create a physical folder, website, server, or database for each Store.

## Device Identity

A Tablet is identified by a `device_id` and a device credential/token after pairing.

The Store URL identifies the Store.

The Device credential identifies the physical Tablet.

These are separate concepts.

## Subdomains

Subdomains are **not the primary architecture for MVP**.

A future optional alias may be supported:

```text
abc.display.example.com
```

but the application must never depend on subdomains for tenant isolation.

## Multi-Tenant Resolution

Store isolation must be based on validated Store and Device identity, authorization, and database relationships.

Example:

```text
/s/abc
    |
    v
store slug = abc
    |
    v
Store ID = 123
    |
    v
Query only Store 123 data
```

For Devices:

```text
Device Token
    |
    v
Device ID
    |
    v
Store ID
    |
    v
Query only that Store's data
```

# 5. User Roles

## 5.1 Super Admin

ดูแล Platform:

-   ร้านค้า
-   Tenant
-   Device
-   System settings
-   Platform status

## 5.2 Store Admin

ดูแลร้านของตัวเอง:

-   Employees
-   Employee images
-   Employee status
-   Display settings
-   Content
-   Devices
-   QR pairing

## 5.3 Display Device

ไม่ใช่ human user

มี:

-   device_id
-   store_id
-   device_token / credential
-   device_name
-   status
-   last_seen
-   configuration

------------------------------------------------------------------------

# 6. Core Features

## FG01 --- Authentication

ต้องมี:

-   Login
-   Logout
-   Session/token
-   Password hashing
-   Role
-   Authorization

ห้ามให้ Store Admin เข้าถึงข้อมูลของร้านอื่น

------------------------------------------------------------------------

## FG02 --- Store Management

ข้อมูลขั้นต่ำ:

``` text
Store
- id
- tenant_id
- name
- slug
- logo
- status
- timezone
- created_at
- updated_at
```

`slug` ต้อง unique

ตัวอย่าง:

``` text
abc
xyz
shop001
```

------------------------------------------------------------------------

## FG03 --- Employee Management

ข้อมูลขั้นต่ำ:

``` text
Employee
- id
- store_id
- name
- nickname
- image_url
- status
- display_order
- active
- created_at
- updated_at
```

Status:

``` text
available
busy
break
offline
```

ต้องสามารถ:

-   เพิ่ม
-   แก้ไข
-   ลบ
-   เปิด/ปิด
-   Upload รูป
-   เปลี่ยนลำดับ
-   เปลี่ยนสถานะ

------------------------------------------------------------------------

# 7. Display Screen

Display เป็นหน้าหลักสำหรับ Tablet

URL:

``` text
https://{store-slug}.staffdisplay.example.com/display
```

หน้าจอควรออกแบบสำหรับ Landscape Tablet เป็นหลัก แต่ต้องรองรับ Portrait ด้วย

## Display States

``` text
LOADING
  |
  v
CONNECTING
  |
  v
DISPLAYING
  |
  +---- connection lost
  |          |
  |          v
  |       OFFLINE
  |          |
  +----------+
```

เมื่อ Internet ขาด:

-   แสดงข้อมูลล่าสุดที่ Cache ไว้
-   ไม่ควรกลายเป็นหน้าขาว
-   เมื่อ Internet กลับมาให้ Sync อัตโนมัติ

------------------------------------------------------------------------

# 8. Display Content

MVP ให้รองรับอย่างน้อย:

### Staff Slide

แสดงพนักงาน:

``` text
[Photo] [Photo] [Photo] [Photo]

 Name    Name    Name    Name
 Status  Status  Status  Status
```

### Promotion Slide

-   รูป
-   ข้อความ
-   ราคา
-   Promotion

### QR Slide

-   QR Code
-   ข้อความ
-   URL

### Store Information

-   ชื่อร้าน
-   Logo
-   เวลาเปิด
-   ข้อมูลติดต่อ

------------------------------------------------------------------------

# 9. Display Playlist

ให้ Admin กำหนดลำดับได้:

``` text
Staff
Promotion
QR
Staff
Store Info
```

แต่ละ Slide มี:

``` text
type
duration
enabled
display_order
```

ตัวอย่าง:

``` text
Staff       5 sec
Promotion   8 sec
QR          10 sec
```

Display จะวนซ้ำอัตโนมัติ

------------------------------------------------------------------------

# 10. Device Pairing

Tablet ใหม่ต้อง Pair กับร้าน

Flow:

``` text
Store Admin
   |
   v
Devices
   |
   v
Add Device
   |
   v
Generate Pairing Code / QR
   |
   v
Tablet
   |
   v
Scan / Enter Code
   |
   v
Confirm Store
   |
   v
Device Registered
```

หลัง Pair แล้ว Tablet ได้ credential ของตัวเอง

ไม่ควรเก็บ Store Admin password บน Tablet

------------------------------------------------------------------------

# 11. Device Management

ข้อมูล:

``` text
Device
- id
- store_id
- name
- device_token_hash
- platform
- app_version
- orientation
- status
- last_seen_at
- created_at
- updated_at
- display_mode
- items_per_page
- auto_slide
- slide_interval
- loop
- show_employee_status
```

Admin ต้องเห็น:

``` text
Tablet-01
Online
Last seen: ...
Version: ...
```

สามารถ:

-   Rename
-   Disable
-   Revoke credential
-   Generate pairing ใหม่
-   ดู Last Seen

------------------------------------------------------------------------

# 12. Realtime

แนะนำ WebSocket หรือ technology ที่ project เลือกใช้

Event ตัวอย่าง:

``` text
employee.updated
employee.deleted
employee.status_changed
display.settings_changed
playlist.updated
content.updated
device.revoked
```

Flow:

``` text
Admin
  |
  | Update Employee
  v
Backend
  |
  +--> PostgreSQL
  |
  +--> Realtime Event
            |
            v
         Tablet
            |
            v
       Refresh Display
```

ไม่ควรให้ Tablet polling ถี่เกินไป

------------------------------------------------------------------------

# 13. PWA Requirements

PWA ต้องมี:

-   manifest
-   service worker
-   installable
-   offline cache
-   local data cache
-   automatic reconnect
-   fullscreen-friendly UI
-   responsive layout
-   tablet optimization

Display mode ต้องไม่มี Admin navigation

------------------------------------------------------------------------

# 14. Local Cache

Tablet ต้อง cache อย่างน้อย:

``` text
Store information
Employee list
Employee images
Display playlist
Display settings
Content
Device configuration
```

เมื่อ Offline:

``` text
Network OFF
    |
    v
Use cached data
    |
    v
Continue displaying
```

เมื่อ Online:

``` text
Network ON
    |
    v
Reconnect
    |
    v
Fetch changes
    |
    v
Update cache
```

------------------------------------------------------------------------

# 15. Security

ข้อกำหนดสำคัญ:

1.  ทุก API ต้องตรวจ authorization
2.  Store Admin ห้ามอ่าน/แก้ข้อมูลร้านอื่น
3.  Device credential ต้อง revoke ได้
4.  ห้ามส่ง Admin password ไป Tablet
5.  Password ต้อง hash
6.  Upload file ต้อง validate type/size
7.  จำกัด image MIME type
8.  ป้องกัน path traversal
9.  Validate slug
10. Rate limit authentication/pairing endpoints
11. HTTPS production
12. Audit log สำหรับ action สำคัญ

------------------------------------------------------------------------

# 16. Suggested Database

เริ่มต้น:

``` text
tenants
stores
users
employees
devices
display_slides
display_settings
media_assets
pairing_codes
audit_logs
```

Relationship:

``` text
tenants
   |
   +--- stores
          |
          +--- employees
          |
          +--- devices
          |
          +--- display_slides
          |
          +--- display_settings
```

------------------------------------------------------------------------

# 17. API Design

Base:

``` text
/api/v1
```

Authentication:

``` text
POST   /auth/login
POST   /auth/logout
GET    /auth/me
```

Stores:

``` text
GET    /stores
GET    /stores/:id
POST   /stores
PUT    /stores/:id
DELETE /stores/:id
```

Employees:

``` text
GET    /stores/:storeId/employees
POST   /stores/:storeId/employees
PUT    /employees/:id
DELETE /employees/:id
PATCH  /employees/:id/status
PATCH  /employees/:id/order
```

Devices:

``` text
GET    /stores/:storeId/devices
POST   /stores/:storeId/devices/pairing
POST   /devices/pair
POST   /devices/:id/revoke
PATCH  /devices/:id
```

Display:

``` text
GET    /display/bootstrap
GET    /display/config
GET    /display/content
```

Realtime:

``` text
/ws
```

------------------------------------------------------------------------

# 18. Bootstrap API

Tablet ไม่ควรยิง API จำนวนมากตอนเริ่มต้น

ควรมี:

``` text
GET /api/v1/display/bootstrap
```

Response แนวคิด:

``` json
{
  "store": {},
  "device": {},
  "employees": [],
  "playlist": [],
  "settings": {},
  "content": [],
  "server_time": "..."
}
```

หลังจากนั้น Realtime แจ้งเฉพาะการเปลี่ยนแปลง

------------------------------------------------------------------------

# 19. Image / Media Strategy

อย่าเก็บ binary image ใหญ่ ๆ ใน PostgreSQL โดยตรงใน MVP

Database เก็บ:

``` text
media_assets
- id
- store_id
- file_name
- storage_key
- mime_type
- width
- height
- size
```

Image storage สามารถเริ่มจาก local storage ใน Development และออกแบบ
abstraction เพื่อย้ายไป Object Storage ภายหลัง

เช่น:

``` text
MediaStorage interface
    |
    +-- LocalStorage
    +-- S3CompatibleStorage (future)
```

------------------------------------------------------------------------

# 20. Admin UI

เมนูขั้นต่ำ:

``` text
Dashboard

Store
 ├── Profile
 └── Settings

Employees
 ├── List
 └── Add/Edit

Display
 ├── Playlist
 ├── Settings
 └── Preview

Devices
 ├── Device List
 └── Pair Device

Media
```

Dashboard แสดง:

``` text
Employees: 12
Devices: 3
Online: 2
Offline: 1
```

------------------------------------------------------------------------

# 21. Display UI/UX

หลักการ:

-   รูปใหญ่
-   อ่านได้จากระยะไกล
-   ปุ่มน้อยที่สุด
-   ไม่มี unnecessary controls
-   Auto transition
-   Smooth animation
-   รองรับ 16:9
-   รองรับ 4:3
-   รองรับ portrait
-   ป้องกัน screen burn-in ด้วย motion/rotation ที่เหมาะสม

------------------------------------------------------------------------

# 22. Project Structure

แนะนำแยก frontend/backend:

``` text
StaffDisplay/
|
+-- backend/
|   +-- cmd/
|   +-- internal/
|   +-- migrations/
|   +-- configs/
|   +-- deploy/
|   +-- tests/
|
+-- frontend/
|   +-- src/
|   |   +-- app/
|   |   +-- components/
|   |   +-- features/
|   |   +-- pages/
|   |   +-- services/
|   |   +-- db/
|   |   +-- pwa/
|   |
|   +-- public/
|
+-- docs/
|   +-- BLUEPRINT.md
|   +-- API.md
|   +-- DATABASE.md
|   +-- DEPLOYMENT.md
|
+-- README.md
```

------------------------------------------------------------------------

# 23. Recommended Technology

## Frontend

``` text
React
Vite
TypeScript
PWA
```

## Backend

``` text
Go
REST API
WebSocket / SignalR-style realtime
```

## Database

``` text
PostgreSQL
```

## Reverse Proxy

``` text
Nginx
```

## Production

``` text
Linux VPS
HTTPS
PostgreSQL
Go API
Static PWA
```

หมายเหตุ: Technology สามารถปรับได้หาก repository ที่ Cline ตรวจพบว่ามี stack
อยู่แล้ว ให้ **ยึด existing project stack ก่อน** และห้ามเปลี่ยน framework
โดยไม่มีเหตุผล

------------------------------------------------------------------------

# 24. Development Rules for Cline

Cline ต้องปฏิบัติตาม:

1.  อ่าน `BLUEPRINT.md` ก่อนเริ่มงาน
2.  ตรวจ repository ปัจจุบันก่อนแก้ไข
3.  ห้ามสร้าง architecture ใหม่ซ้ำกับของเดิม
4.  ห้ามเปลี่ยน framework โดยพลการ
5.  ทำงานเป็น Feature Group
6.  แต่ละ Feature Group ต้อง build/test ผ่าน
7.  ห้ามแก้ไฟล์นอก scope โดยไม่จำเป็น
8.  ก่อน commit ต้องตรวจ git diff
9.  ห้ามลบข้อมูลหรือ migration เดิมโดยไม่ตรวจสอบ
10. ต้อง update documentation เมื่อ architecture เปลี่ยน
11. Security boundary ของ tenant ต้องถูก test
12. ทุก API สำคัญต้องมี test
13. Display ต้องมี offline/reconnect test
14. Pairing ต้องมี revoke test
15. ห้าม hard-code production domain

------------------------------------------------------------------------

# 25. Feature Group Roadmap

## Phase 1 --- Foundation

FG1. Project setup\
FG2. Database\
FG3. Configuration\
FG4. Authentication\
FG5. Basic tenant/store model

Definition of Done:

-   Project build
-   DB migration
-   Login
-   Authorization
-   Store isolation test

------------------------------------------------------------------------

## Phase 2 --- Employee

FG6. Employee CRUD\
FG7. Image upload\
FG8. Employee status\
FG9. Display ordering

Definition of Done:

-   CRUD works
-   Image upload works
-   Store isolation verified

------------------------------------------------------------------------

## Phase 3 --- Display

FG10. Display page\
FG11. Responsive tablet UI\
FG12. Staff slide\
FG13. Promotion slide\
FG14. QR slide\
FG15. Playlist

Definition of Done:

-   Tablet can display store content
-   Auto rotation works
-   Responsive layout works

------------------------------------------------------------------------

## Phase 4 --- Device

FG16. Device model\
FG17. Pairing code\
FG18. QR pairing\
FG19. Device authentication\
FG20. Device management\
FG21. Revoke

Definition of Done:

-   New tablet can pair
-   Device receives only its store data
-   Revoked device loses access

------------------------------------------------------------------------

## Phase 5 --- Realtime

FG22. WebSocket\
FG23. Employee events\
FG24. Playlist events\
FG25. Display settings events\
FG26. Reconnect

Definition of Done:

Admin update -\> Tablet updates without manual refresh.

------------------------------------------------------------------------

## Phase 6 --- Offline/PWA

FG27. Service Worker\
FG28. IndexedDB/local cache\
FG29. Offline display\
FG30. Automatic sync

Definition of Done:

Internet disconnect -\> display continues.

------------------------------------------------------------------------

## Phase 7 --- Production

FG31. Nginx\
FG32. HTTPS\
FG33. Wildcard DNS\
FG34. Backup\
FG35. Logging\
FG36. Monitoring\
FG37. Security review

------------------------------------------------------------------------

# 26. Critical Acceptance Tests

## Multi-tenant isolation

``` text
Store A login
  -> can read A
  -> cannot read B
  -> cannot update B
```

## Device isolation

``` text
Device A
  -> sees Store A

Device B
  -> sees Store B
```

## Revocation

``` text
Device paired
   |
Revoke
   |
Device cannot reconnect
```

## Realtime

``` text
Admin changes employee
   |
Backend
   |
Event
   |
Tablet updates
```

## Offline

``` text
Online
   |
Cache
   |
Network OFF
   |
Display continues
```

## Recovery

``` text
Network OFF
   |
Network ON
   |
Reconnect
   |
Sync
   |
Display current data
```

------------------------------------------------------------------------

# 27. Non-Goals for MVP

ยังไม่ต้องทำ:

-   ESP32
-   Hardware sensor
-   Face recognition
-   AI recommendation
-   Payment
-   POS
-   Booking system
-   Membership
-   LINE integration
-   Subscription billing
-   Complex analytics

ให้ MVP พิสูจน์ก่อนว่า:

``` text
ร้านสร้างบัญชี
      ↓
เพิ่มพนักงาน
      ↓
อัปโหลดรูป
      ↓
Pair Tablet
      ↓
Tablet แสดงพนักงาน
      ↓
Admin เปลี่ยนข้อมูล
      ↓
Tablet เปลี่ยนตาม Realtime
```

------------------------------------------------------------------------

# 28. Future Expansion

หลัง MVP สำเร็จ สามารถเพิ่ม:

``` text
AI-generated promotion
QR campaign
Customer interaction
Advertisement
Multiple playlists
Scheduled content
Analytics
Remote screenshot
Remote restart
Device health
Subscription
Custom domain
White-label
```

Custom domain ในอนาคต:

``` text
display.shopabc.com
```

แต่ **ไม่ทำใน MVP**

------------------------------------------------------------------------

# 29. First Implementation Task for Cline

เมื่อเริ่ม project ให้ Cline ทำตามลำดับนี้:

### Step 1

ตรวจ repository:

``` text
- existing files
- existing framework
- package manager
- database
- configuration
- deployment
```

### Step 2

สร้าง/ปรับ:

``` text
docs/BLUEPRINT.md
```

ให้ตรงกับ project จริง

### Step 3

สร้าง Foundation

``` text
Backend
Database
Frontend
Configuration
```

### Step 4

ทำ Authentication + Store/Tenant

### Step 5

ทำ Employee CRUD

### Step 6

ทำ Display

### Step 7

ทำ Device Pairing

### Step 8

ทำ Realtime

### Step 9

ทำ Offline/PWA

### Step 10

ทำ Production deployment

------------------------------------------------------------------------

# 30. Golden Rule

**อย่าทำระบบให้เป็น "เว็บไซต์ของร้านหนึ่งร้าน"**

ให้คิดตั้งแต่ต้นว่า:

``` text
ONE PLATFORM
      |
      +---- Store A
      |
      +---- Store B
      |
      +---- Store C
      |
      +---- Store N
```

ทุก feature ต้องตอบคำถาม:

> "ถ้ามี 1,000 ร้าน ระบบนี้ยังทำงานได้หรือไม่?"

และต้องไม่แก้ปัญหาด้วยการสร้าง Server หรือ Database แยกร้าน เว้นแต่มีเหตุผลด้าน
scale/security ในอนาคต

------------------------------------------------------------------------

# 31. Definition of MVP Complete

ถือว่า MVP เสร็จเมื่อสามารถทำ flow ต่อไปนี้ได้ครบ:

``` text
1. Admin Login
       ↓
2. Create Store
       ↓
3. Add Employee
       ↓
4. Upload Employee Image
       ↓
5. Create Display Device
       ↓
6. Pair Tablet
       ↓
7. Tablet opens Display
       ↓
8. Employee appears
       ↓
9. Configure 1/4/8/12 layout and Stand/Handheld mode
       ↓
10. Admin changes Employee
       ↓
11. Tablet updates automatically
       ↓
12. Internet disconnects
       ↓
13. Tablet continues showing cached data
       ↓
14. Internet returns
       ↓
15. Tablet reconnects and syncs
```

**จบ MVP เมื่อ Flow นี้ผ่านทั้ง Functional Test และ Security/Tenant Isolation
Test**


# 32. Final Architecture Decision

The MVP architecture is:

```text
Single Domain
    |
    +-- /app              Admin
    +-- /s/{store-slug}   Store Display
    +-- /setup            Device Setup
    +-- /api              Backend API
```

Multi-tenant isolation is implemented with:

```text
Tenant / Store ID
        +
Device ID / Device Token
        +
Authorization
```

Do not create separate applications, servers, databases, or physical folders per Store.

Subdomains may be added later as optional aliases without changing the underlying tenant model.

---

# 33. Cline Execution Rule

Cline must treat this document as the project Source of Truth.

Before implementation:

1. Inspect the repository.
2. Identify existing technology and reusable code.
3. Identify conflicts with this Blueprint.
4. Do not replace existing frameworks without justification.
5. Implement one Feature Group at a time.
6. Build and test after each Feature Group.
7. Review `git diff`.
8. Commit only after the Feature Group passes its acceptance criteria.
9. Update documentation when architecture changes.
10. Do not implement ESP32 or external hardware integration for MVP.
