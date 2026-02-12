# DOMAIN MANAGEMENT MODULE — Technical Specification

**Multi-Provider Domain Reselling Platform**

Version 1.0 | February 2026
Classification: Internal / Technical

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [System Architecture](#2-system-architecture)
3. [Platform REST API Specification](#3-platform-rest-api-specification)
4. [Provider Abstraction Layer](#4-provider-abstraction-layer)
5. [DNS Architecture](#5-dns-architecture)
6. [Billing & Pricing Engine](#6-billing--pricing-engine)
7. [Security Considerations](#7-security-considerations)
8. [Implementation Roadmap](#8-implementation-roadmap)
9. [Recommended Technology Stack](#9-recommended-technology-stack)
10. [Cost Analysis](#10-cost-analysis)

---

## 1. Executive Summary

This specification defines the Domain Management Module for the hosting platform, enabling customers to search, register, transfer, renew, and manage domain names directly within the platform. The module implements a multi-provider abstraction layer that normalizes the APIs of multiple domain registrar partners into a single unified interface.

### Key Design Principles

- **Provider Agnostic:** A unified abstraction layer so the platform is never locked to a single registrar
- **Cost Optimization:** Route registrations to the cheapest provider per TLD automatically
- **Seamless Integration:** Domains purchased through the platform auto-configure DNS for hosted services
- **White-Label:** End users never see the upstream registrar; all branding is yours
- **Resilient:** If one provider is down, the system fails over to an alternative provider

### Supported Registrar Providers (Initial Launch)

| Provider | API Type | TLDs | Signup Cost | Best For |
|---|---|---|---|---|
| Name.com | REST (CORE v1) | 1,000+ | Free | Modern REST API, fast responses |
| OpenSRS (Tucows) | XML over HTTPS | 800+ | Free | Battle-tested, huge ecosystem |
| CentralNic Reseller | HTTPS / EPP / SOAP | 1,100+ | Free | Largest TLD selection |
| Domain Name API | REST | 800+ | Free | Cheapest pricing, no minimums |

> Additional providers can be added by implementing the `RegistrarAdapter` interface (see Section 4).

---

## 2. System Architecture

### 2.1 High-Level Architecture

The Domain Management Module sits between the platform frontend and multiple upstream registrar APIs. All domain operations flow through a Provider Router that selects the optimal registrar based on TLD, price, and availability.

```
┌─────────────────────────────────────────────────────────┐
│                    PLATFORM UI / API                     │
│              (Domain Search, Management Dashboard)        │
└──────────────────────────┬──────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────┐
│                    DOMAIN SERVICE                         │
│         (Business Logic, Pricing, Validation, Billing)    │
└──────────────────────────┬──────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────┐
│                   PROVIDER ROUTER                         │
│      (TLD Mapping, Cost Optimization, Failover)           │
└───┬──────────┬──────────┬──────────┬────────────────────┘
    │          │          │          │
┌───▼───┐ ┌───▼───┐ ┌───▼────┐ ┌──▼──────┐
│Name.com│ │OpenSRS│ │Central │ │DomainName│
│Adapter │ │Adapter│ │Nic Adpt│ │API Adpt  │
└───┬───┘ └───┬───┘ └───┬────┘ └──┬──────┘
    │         │         │          │
    ▼         ▼         ▼          ▼
  REST       XML      HTTPS      REST
  API        API       API        API
```

### Architecture Layers

| Layer | Component | Responsibility |
|---|---|---|
| Presentation | Platform UI / API | Domain search UI, management dashboard, REST endpoints |
| Application | Domain Service | Business logic, pricing, validation, billing integration |
| Abstraction | Provider Router | TLD-to-provider mapping, failover, cost optimization |
| Adapter | Registrar Adapters | Provider-specific API translation (Name.com, OpenSRS, etc.) |
| Persistence | Database | Domain records, DNS zones, transaction logs, provider configs |

### 2.2 Domain Registration Flow

1. User searches for domain via platform UI
2. Domain Service fans out availability check to all configured providers (parallel)
3. Provider Router aggregates results, selects cheapest available provider
4. User confirms purchase; platform charges user account (with markup)
5. Domain Service calls the selected registrar adapter to register the domain
6. Adapter translates the request to provider-specific API format and executes
7. On success: DNS records are auto-configured to point to the platform
8. Domain record is saved to the local database with provider metadata
9. Confirmation email sent to user with domain details

### 2.3 Database Schema

#### `domains`

| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| user_id | UUID | FK to users table |
| domain_name | VARCHAR(253) | Full domain name (e.g., example.com) |
| tld | VARCHAR(63) | Top-level domain extracted |
| provider | VARCHAR(50) | Registrar provider slug (e.g., namecom, opensrs) |
| provider_domain_id | VARCHAR(255) | Provider-side domain identifier |
| status | ENUM | active, pending, expired, transferred_out, suspended |
| registered_at | TIMESTAMP | Original registration date |
| expires_at | TIMESTAMP | Expiration date |
| auto_renew | BOOLEAN | Auto-renewal enabled (default: true) |
| whois_privacy | BOOLEAN | WHOIS privacy protection enabled |
| locked | BOOLEAN | Domain transfer lock status |
| auth_code | VARCHAR(255) | Transfer auth code (encrypted at rest) |
| cost_price | DECIMAL(10,2) | What we paid the registrar |
| sell_price | DECIMAL(10,2) | What the customer paid |
| created_at | TIMESTAMP | Record creation |
| updated_at | TIMESTAMP | Last modification |

#### `dns_records`

| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| domain_id | UUID | FK to domains table |
| type | ENUM | A, AAAA, CNAME, MX, TXT, NS, SRV, CAA |
| name | VARCHAR(253) | Record hostname (e.g., @ or www) |
| value | TEXT | Record value (IP, hostname, text, etc.) |
| ttl | INTEGER | Time to live in seconds (default: 3600) |
| priority | INTEGER | Priority for MX/SRV records |
| managed | BOOLEAN | True if auto-managed by platform (not user-editable) |
| created_at | TIMESTAMP | Record creation |

#### `domain_transactions`

| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| domain_id | UUID | FK to domains table |
| type | ENUM | registration, renewal, transfer_in, transfer_out |
| provider | VARCHAR(50) | Which registrar fulfilled this |
| provider_txn_id | VARCHAR(255) | Provider-side transaction ID |
| amount_cost | DECIMAL(10,2) | Cost to us |
| amount_charged | DECIMAL(10,2) | Amount charged to customer |
| period_years | INTEGER | Registration/renewal period |
| status | ENUM | pending, completed, failed, refunded |
| error_message | TEXT | Error details if failed |
| created_at | TIMESTAMP | Transaction time |

#### `provider_configs`

| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| provider_slug | VARCHAR(50) | Unique provider identifier |
| display_name | VARCHAR(100) | Human-readable name |
| api_endpoint | VARCHAR(255) | Production API base URL |
| test_endpoint | VARCHAR(255) | Sandbox/test API base URL |
| credentials | JSONB | Encrypted API keys, usernames, tokens |
| enabled | BOOLEAN | Whether this provider is active |
| priority | INTEGER | Default priority (lower = preferred) |
| supported_tlds | JSONB | Array of supported TLD strings |
| pricing_cache | JSONB | Cached TLD pricing from provider |
| pricing_updated_at | TIMESTAMP | When pricing was last fetched |
| rate_limit_rpm | INTEGER | Max requests per minute |

#### `tld_routing`

| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| tld | VARCHAR(63) | Top-level domain (e.g., com, io, dev) |
| preferred_provider | VARCHAR(50) | FK to provider_configs.provider_slug |
| fallback_provider | VARCHAR(50) | Backup provider if preferred fails |
| our_sell_price | DECIMAL(10,2) | What we charge customers |
| our_renewal_price | DECIMAL(10,2) | What we charge for renewal |
| is_premium | BOOLEAN | Whether this TLD has premium pricing |
| enabled | BOOLEAN | Whether we offer this TLD |

---

## 3. Platform REST API Specification

These are the public-facing REST API endpoints exposed by the platform to customers. All endpoints require authentication via Bearer token.

### 3.1 Domain Search & Availability

**`POST /api/v1/domains/search`**

Search for domain availability across all configured providers. Returns availability, pricing, and suggested alternatives.

**Request Body:**

```json
{
  "query": "myapp.com",
  "tlds": ["com", "io", "dev", "app"],
  "suggestions": true,
  "max_results": 20
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| query | string | Yes | Domain name to search (e.g., myapp.com) |
| tlds | string[] | No | Specific TLDs to check (default: popular TLDs) |
| suggestions | boolean | No | Include AI-generated alternative suggestions |
| max_results | integer | No | Maximum results to return (default: 20) |

**Response (200 OK):**

```json
{
  "results": [
    {
      "domain": "myapp.com",
      "available": true,
      "premium": false,
      "price": {
        "registration": 12.99,
        "renewal": 12.99,
        "currency": "USD"
      }
    },
    {
      "domain": "myapp.io",
      "available": true,
      "premium": false,
      "price": {
        "registration": 32.00,
        "renewal": 32.00,
        "currency": "USD"
      }
    }
  ],
  "suggestions": [
    { "domain": "getmyapp.com", "available": true, "price": { "registration": 12.99 } },
    { "domain": "myapp.dev", "available": true, "price": { "registration": 16.00 } }
  ]
}
```

### 3.2 Domain Registration

**`POST /api/v1/domains/register`**

Register a new domain. The platform handles provider selection, payment processing, and DNS configuration automatically.

**Request Body:**

```json
{
  "domain": "myapp.com",
  "period_years": 1,
  "auto_renew": true,
  "whois_privacy": true,
  "contact": {
    "first_name": "John",
    "last_name": "Doe",
    "email": "john@example.com",
    "phone": "+14155551234",
    "address": {
      "street": "123 Main St",
      "city": "San Francisco",
      "state": "CA",
      "zip": "94102",
      "country": "US"
    }
  },
  "nameservers": null
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| domain | string | Yes | Domain name to register |
| period_years | integer | No | Registration period 1-10 (default: 1) |
| auto_renew | boolean | No | Enable auto-renewal (default: true) |
| whois_privacy | boolean | No | Enable WHOIS privacy (default: true) |
| contact.first_name | string | Yes | Registrant first name |
| contact.last_name | string | Yes | Registrant last name |
| contact.email | string | Yes | Registrant email |
| contact.phone | string | Yes | Phone in E.164 format |
| contact.address | object | Yes | Street, city, state, zip, country |
| nameservers | string[] | No | Custom NS (default: platform NS) |

**Response (201 Created):**

```json
{
  "domain_id": "d_8f3a2b1c",
  "domain": "myapp.com",
  "status": "active",
  "expires_at": "2027-02-09T00:00:00Z",
  "nameservers": ["ns1.platform.com", "ns2.platform.com"],
  "transaction": {
    "id": "txn_9c4d3e2f",
    "amount": 12.99,
    "currency": "USD"
  }
}
```

### 3.3 Domain Transfer

**`POST /api/v1/domains/transfer`**

Transfer an existing domain from another registrar into the platform.

**Request Body:**

```json
{
  "domain": "existing-site.com",
  "auth_code": "aB3$kL9mNp",
  "auto_renew": true
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| domain | string | Yes | Domain to transfer |
| auth_code | string | Yes | EPP/transfer authorization code |
| contact | object | No | Override registrant contact |
| auto_renew | boolean | No | Enable auto-renewal (default: true) |

**Response (202 Accepted):**

```json
{
  "domain_id": "d_7e2b1a3c",
  "domain": "existing-site.com",
  "status": "transfer_pending",
  "estimated_completion": "2026-02-16T00:00:00Z",
  "transaction": {
    "id": "txn_5a6b7c8d",
    "amount": 12.99
  }
}
```

### 3.4 Domain Management

| Endpoint | Method | Description |
|---|---|---|
| `/api/v1/domains` | GET | List all domains for authenticated user (paginated) |
| `/api/v1/domains/{domain_id}` | GET | Get detailed domain info (status, DNS, expiration) |
| `/api/v1/domains/{domain_id}` | PATCH | Update settings (auto_renew, locked, whois_privacy) |
| `/api/v1/domains/{domain_id}/renew` | POST | Manually renew for additional period |
| `/api/v1/domains/{domain_id}` | DELETE | Cancel/delete domain (subject to grace periods) |

**`PATCH /api/v1/domains/{domain_id}` Request:**

```json
{
  "auto_renew": false,
  "locked": true,
  "whois_privacy": true
}
```

### 3.5 DNS Management

| Endpoint | Method | Description |
|---|---|---|
| `/api/v1/domains/{domain_id}/dns` | GET | List all DNS records |
| `/api/v1/domains/{domain_id}/dns` | POST | Create a new DNS record |
| `/api/v1/domains/{domain_id}/dns/{record_id}` | PUT | Update an existing record |
| `/api/v1/domains/{domain_id}/dns/{record_id}` | DELETE | Delete a record (managed records protected) |

**`POST /api/v1/domains/{domain_id}/dns` Request:**

```json
{
  "type": "A",
  "name": "@",
  "value": "65.21.123.45",
  "ttl": 3600
}
```

```json
{
  "type": "MX",
  "name": "@",
  "value": "mail.example.com",
  "ttl": 3600,
  "priority": 10
}
```

**Response (201 Created):**

```json
{
  "id": "rec_1a2b3c4d",
  "type": "A",
  "name": "@",
  "value": "65.21.123.45",
  "ttl": 3600,
  "managed": false,
  "created_at": "2026-02-09T22:30:00Z"
}
```

### 3.6 Nameserver Management

| Endpoint | Method | Description |
|---|---|---|
| `/api/v1/domains/{domain_id}/nameservers` | GET | Get current nameserver config |
| `/api/v1/domains/{domain_id}/nameservers` | PUT | Update nameservers (array of 2-6 hostnames) |

**`PUT /api/v1/domains/{domain_id}/nameservers` Request:**

```json
{
  "nameservers": [
    "ns1.platform.com",
    "ns2.platform.com"
  ]
}
```

### 3.7 Error Responses

All error responses follow a consistent format:

```json
{
  "error": {
    "code": "domain_unavailable",
    "message": "The domain myapp.com is not available for registration",
    "details": {
      "provider": "namecom",
      "provider_error": "Domain is already registered"
    }
  }
}
```

| HTTP Code | Error Code | Description |
|---|---|---|
| 400 | invalid_domain | Malformed or invalid domain name |
| 400 | invalid_tld | TLD not supported by the platform |
| 402 | insufficient_funds | User account balance too low |
| 404 | domain_not_found | Domain not in user's account |
| 409 | domain_unavailable | Domain already registered |
| 409 | transfer_locked | Domain is transfer-locked (60-day rule) |
| 422 | invalid_contact | Missing or invalid registrant contact data |
| 429 | rate_limited | Too many search requests |
| 502 | provider_error | Upstream registrar API failure |
| 503 | provider_unavailable | All providers for this TLD are down |

---

## 4. Provider Abstraction Layer

The core of the multi-provider architecture is the `RegistrarAdapter` interface. Each registrar provider implements this interface, translating unified platform operations into provider-specific API calls.

### 4.1 RegistrarAdapter Interface

```python
from abc import ABC, abstractmethod
from dataclasses import dataclass
from typing import Optional

@dataclass
class AvailabilityResult:
    domain: str
    available: bool
    premium: bool
    price_register: Optional[float]
    price_renew: Optional[float]
    currency: str = "USD"

@dataclass
class RegistrationResult:
    success: bool
    provider_domain_id: str
    registered_at: str
    expires_at: str
    nameservers: list[str]
    transaction_id: str
    error: Optional[str] = None

@dataclass
class DnsRecord:
    id: str
    type: str        # A, AAAA, CNAME, MX, TXT, NS, SRV, CAA
    name: str
    value: str
    ttl: int = 3600
    priority: Optional[int] = None

class RegistrarAdapter(ABC):
    """Base interface for all registrar provider adapters."""

    @abstractmethod
    async def check_availability(self, domain: str) -> AvailabilityResult:
        """Check if a single domain is available."""
        ...

    @abstractmethod
    async def bulk_check(self, domains: list[str]) -> list[AvailabilityResult]:
        """Check multiple domains at once."""
        ...

    @abstractmethod
    async def get_price(self, domain: str, action: str) -> dict:
        """Get price for register/renew/transfer action."""
        ...

    @abstractmethod
    async def register(self, domain: str, contact: dict, options: dict) -> RegistrationResult:
        """Register a new domain."""
        ...

    @abstractmethod
    async def renew(self, domain: str, period_years: int) -> dict:
        """Renew an existing domain."""
        ...

    @abstractmethod
    async def transfer(self, domain: str, auth_code: str, contact: dict) -> dict:
        """Initiate inbound transfer."""
        ...

    @abstractmethod
    async def get_domain(self, domain: str) -> dict:
        """Get domain details from provider."""
        ...

    @abstractmethod
    async def set_nameservers(self, domain: str, nameservers: list[str]) -> None:
        """Update nameservers at registry."""
        ...

    @abstractmethod
    async def set_lock(self, domain: str, locked: bool) -> None:
        """Set/unset transfer lock."""
        ...

    @abstractmethod
    async def get_auth_code(self, domain: str) -> str:
        """Retrieve EPP auth code."""
        ...

    @abstractmethod
    async def set_whois_privacy(self, domain: str, enabled: bool) -> None:
        """Toggle WHOIS privacy."""
        ...

    @abstractmethod
    async def get_dns_records(self, domain: str) -> list[DnsRecord]:
        """List DNS records at provider."""
        ...

    @abstractmethod
    async def create_dns_record(self, domain: str, record: DnsRecord) -> DnsRecord:
        """Create DNS record at provider."""
        ...

    @abstractmethod
    async def update_dns_record(self, domain: str, record_id: str, record: DnsRecord) -> DnsRecord:
        """Update DNS record."""
        ...

    @abstractmethod
    async def delete_dns_record(self, domain: str, record_id: str) -> None:
        """Delete DNS record."""
        ...

    @abstractmethod
    async def get_tld_pricing(self) -> list[dict]:
        """Fetch all TLD prices from provider."""
        ...

    @abstractmethod
    async def health_check(self) -> dict:
        """Verify API connectivity. Returns {healthy: bool, latency_ms: int}."""
        ...
```

### 4.2 Provider-Specific Implementation Notes

#### 4.2.1 Name.com Adapter

- **API:** REST (CORE v1) over HTTPS
- **Base URL:** `https://api.name.com/core/v1`
- **Auth:** HTTP Basic Auth (username + API token)
- **Rate Limit:** 20 req/sec, 3,000 req/hour
- **Sandbox:** `https://api.dev.name.com` (append `-test` to username)
- **Key Endpoints:**
  - `POST /domains:checkAvailability` — domain search
  - `POST /domains` — register domain
  - `GET /domains/{name}` — get domain info
  - `GET /domains/{name}/records` — list DNS records
  - `POST /domains/{name}/records` — create DNS record
  - `POST /domains/{name}:renew` — renew domain
- **Notes:** Modern REST, JSON payloads. Best developer experience. White-label emails available via `api@name.com`. CORE v1 released June 2025; v4 sunsetting in 2026.

```python
class NameComAdapter(RegistrarAdapter):
    BASE_URL = "https://api.name.com/core/v1"
    TEST_URL = "https://api.dev.name.com/core/v1"

    async def check_availability(self, domain: str) -> AvailabilityResult:
        resp = await self.client.post(
            f"{self.base_url}/domains:checkAvailability",
            json={"domainNames": [domain]}
        )
        result = resp.json()["results"][0]
        return AvailabilityResult(
            domain=domain,
            available=result["purchasable"],
            premium=result.get("premium", False),
            price_register=result.get("purchasePrice"),
            price_renew=result.get("renewalPrice"),
        )

    async def register(self, domain: str, contact: dict, options: dict) -> RegistrationResult:
        resp = await self.client.post(
            f"{self.base_url}/domains",
            json={
                "domain": {"domainName": domain},
                "purchasePrice": options.get("price"),
                "years": options.get("period_years", 1),
                "contacts": self._format_contacts(contact),
                "nameservers": options.get("nameservers", self.default_ns),
            }
        )
        data = resp.json()
        return RegistrationResult(
            success=True,
            provider_domain_id=data["domainName"],
            registered_at=data["createDate"],
            expires_at=data["expireDate"],
            nameservers=data.get("nameservers", []),
            transaction_id=data.get("orderId", ""),
        )
```

#### 4.2.2 OpenSRS Adapter

- **API:** XML over HTTPS (XCP protocol)
- **Base URL:** `https://rr-n1-tor.opensrs.net:55443`
- **Auth:** MD5 signature of XML payload + API key, sent via `X-Signature` header
- **Rate Limit:** No published limit (enterprise-grade)
- **Sandbox:** `https://horizon.opensrs.net:55443`
- **Key Commands:**
  - `LOOKUP` — domain availability
  - `SW_REGISTER` — register or transfer domain
  - `RENEW` — renew domain
  - `GET` — get domain info
  - `SET` — update nameservers, contacts
  - `NAME_SUGGEST` — domain suggestions
- **Notes:** XML-based; requires XML builder/parser. Requires IP whitelisting for production. Most mature platform. Port 55443 must be open in firewall.

```python
class OpenSRSAdapter(RegistrarAdapter):
    BASE_URL = "https://rr-n1-tor.opensrs.net:55443"
    TEST_URL = "https://horizon.opensrs.net:55443"

    def _sign_request(self, xml_payload: str) -> str:
        """Generate MD5 signature: md5(md5(xml + key) + key)"""
        inner = hashlib.md5((xml_payload + self.api_key).encode()).hexdigest()
        return hashlib.md5((inner + self.api_key).encode()).hexdigest()

    def _build_xml(self, action: str, obj: str, attributes: dict) -> str:
        """Build OpenSRS XCP XML envelope."""
        # Returns XML with OPS_envelope > header + body > data_block
        ...

    async def check_availability(self, domain: str) -> AvailabilityResult:
        xml = self._build_xml("LOOKUP", "DOMAIN", {"domain": domain})
        resp = await self.client.post(
            self.base_url,
            content=xml,
            headers={
                "Content-Type": "text/xml",
                "X-Username": self.username,
                "X-Signature": self._sign_request(xml),
            }
        )
        result = self._parse_xml(resp.text)
        return AvailabilityResult(
            domain=domain,
            available=result["response_code"] == "210",
            premium=False,
            price_register=None,  # Requires separate price lookup
            price_renew=None,
        )
```

#### 4.2.3 CentralNic Reseller Adapter

- **API:** HTTPS query string API (also supports EPP, SOAP, XRRP)
- **Base URL:** `https://api.rrpproxy.net/api/call`
- **Auth:** Username + password in request params
- **Rate Limit:** No published limit
- **Sandbox:** OT&E test environment available
- **Key Commands:**
  - `CheckDomain` — availability check
  - `AddDomain` — register domain
  - `RenewDomain` — renew
  - `TransferDomain` — initiate transfer
  - `StatusDomain` — get domain info
  - `QueryDomainList` — list domains
  - `ModifyDomain` — update settings
- **Notes:** 1,100+ TLDs, largest selection. Response is key-value text format, not JSON. Hierarchical sub-reseller system available.

```python
class CentralNicAdapter(RegistrarAdapter):
    BASE_URL = "https://api.rrpproxy.net/api/call"

    async def check_availability(self, domain: str) -> AvailabilityResult:
        resp = await self.client.get(
            self.BASE_URL,
            params={
                "s_login": self.username,
                "s_pw": self.password,
                "command": "CheckDomain",
                "domain": domain,
            }
        )
        # Response is key=value text format
        result = self._parse_response(resp.text)
        return AvailabilityResult(
            domain=domain,
            available=result["code"] == "210",
            premium="PREMIUM" in result.get("class", ""),
            price_register=result.get("price"),
            price_renew=result.get("renewalprice"),
        )
```

#### 4.2.4 Domain Name API Adapter

- **API:** REST over HTTPS
- **Auth:** API key via header
- **Rate Limit:** Standard web API limits
- **Sandbox:** Test environment available
- **Key Endpoints:** Standard REST CRUD for domains, DNS, contacts
- **Notes:** Cheapest .com pricing (~$5.99 with rebate program). No minimum deposit. 800+ TLDs. Free WHOIS privacy. GitHub: `github.com/domainreseller`

### 4.3 Provider Router

The Provider Router determines which registrar to use for each domain operation.

```python
class ProviderRouter:
    def __init__(self, db, adapters: dict[str, RegistrarAdapter]):
        self.db = db
        self.adapters = adapters
        self.circuit_breakers: dict[str, CircuitBreaker] = {}

    async def select_provider(self, domain: str, action: str = "register") -> RegistrarAdapter:
        """Select the optimal provider for a domain operation."""
        tld = domain.split(".")[-1]

        # 1. Check TLD routing table for preferred provider
        routing = await self.db.get_tld_routing(tld)
        if routing and routing.preferred_provider:
            provider_slug = routing.preferred_provider
            if self._is_healthy(provider_slug):
                return self.adapters[provider_slug]

        # 2. Fallback provider
        if routing and routing.fallback_provider:
            provider_slug = routing.fallback_provider
            if self._is_healthy(provider_slug):
                return self.adapters[provider_slug]

        # 3. Auto-route: find cheapest healthy provider supporting this TLD
        candidates = []
        for slug, adapter in self.adapters.items():
            if not self._is_healthy(slug):
                continue
            config = await self.db.get_provider_config(slug)
            if tld in config.supported_tlds:
                price = config.pricing_cache.get(tld, {}).get(action, float("inf"))
                candidates.append((slug, price))

        candidates.sort(key=lambda x: x[1])
        if candidates:
            return self.adapters[candidates[0][0]]

        raise ProviderUnavailableError(f"No healthy provider available for .{tld}")

    def _is_healthy(self, provider_slug: str) -> bool:
        cb = self.circuit_breakers.get(provider_slug)
        return cb is None or cb.state != "open"
```

**Circuit Breaker Settings:**

| Parameter | Value | Description |
|---|---|---|
| failure_threshold | 3 | Failures before opening circuit |
| recovery_timeout | 60s | Time before retrying a failed provider |
| half_open_max | 1 | Test requests in half-open state |

---

## 5. DNS Architecture

For a Render-like experience, domains purchased through the platform should automatically work with hosted services.

### 5.1 Two Approaches

| Approach | Pros | Cons | Recommended |
|---|---|---|---|
| Self-hosted DNS (PowerDNS / CoreDNS) | Full control, no per-query cost, instant propagation | Must manage HA, DDoS protection, anycast | For scale (500+ domains) |
| Proxy via provider API | No infrastructure, built-in DDoS, anycast included | API latency for changes, per-domain cost | For MVP |

**Recommended for MVP:** Use the registrar's built-in DNS (Name.com and OpenSRS both provide free DNS hosting with domains). Manage records via their API. Migrate to self-hosted PowerDNS when you reach 500+ domains.

### 5.2 Auto-Configuration

When a user connects a domain to a hosted service, the platform automatically creates the necessary DNS records:

| Service Type | Records Created | Example |
|---|---|---|
| Web Service | A + AAAA to platform LB IP | myapp.com → 65.21.x.x |
| Web Service (www) | CNAME www → root domain | www.myapp.com → myapp.com |
| SSL Certificate | TXT `_acme-challenge` for Let's Encrypt | Automated via certbot |
| Email (if offered) | MX + SPF + DKIM + DMARC TXT records | Full email deliverability |
| Custom Subdomain | CNAME to platform ingress | api.myapp.com → ingress.platform.com |

```python
class DnsAutoConfigurator:
    PLATFORM_IPV4 = "65.21.xxx.xxx"  # Your Hetzner server IP
    PLATFORM_IPV6 = "2a01:xxxx::1"

    async def configure_web_service(self, domain_id: str, service_name: str):
        """Auto-configure DNS for a web service deployment."""
        adapter = await self.get_adapter_for_domain(domain_id)
        domain = await self.db.get_domain(domain_id)

        # Root domain A + AAAA records
        await adapter.create_dns_record(domain.domain_name, DnsRecord(
            type="A", name="@", value=self.PLATFORM_IPV4, ttl=300
        ))
        await adapter.create_dns_record(domain.domain_name, DnsRecord(
            type="AAAA", name="@", value=self.PLATFORM_IPV6, ttl=300
        ))

        # WWW CNAME
        await adapter.create_dns_record(domain.domain_name, DnsRecord(
            type="CNAME", name="www", value=domain.domain_name, ttl=300
        ))

        # Mark as managed
        await self.db.mark_records_managed(domain_id, managed=True)

        # Trigger SSL provisioning
        await self.ssl_manager.provision_certificate(domain.domain_name)
```

### 5.3 SSL/TLS Certificate Automation

Integrate Let's Encrypt via ACME protocol for automatic SSL provisioning:

1. User registers or connects domain
2. Platform creates DNS-01 challenge TXT record via registrar API
3. ACME client validates and issues certificate
4. Certificate is stored and auto-renewed 30 days before expiry
5. Wildcard certificates supported via DNS-01 validation

```python
class SSLManager:
    async def provision_certificate(self, domain: str):
        """Provision SSL certificate via Let's Encrypt DNS-01."""
        # 1. Request challenge from ACME
        challenge = await self.acme.request_challenge(domain, "dns-01")

        # 2. Create TXT record via registrar
        adapter = await self.get_adapter(domain)
        await adapter.create_dns_record(domain, DnsRecord(
            type="TXT",
            name="_acme-challenge",
            value=challenge.validation_token,
            ttl=60
        ))

        # 3. Wait for DNS propagation
        await self._wait_for_propagation(domain, challenge.validation_token)

        # 4. Validate and obtain certificate
        cert = await self.acme.validate_and_finalize(challenge)

        # 5. Store and deploy certificate
        await self.cert_store.save(domain, cert)
        await self.ingress.reload_certificate(domain, cert)

        # 6. Cleanup challenge record
        await adapter.delete_dns_record(domain, challenge_record_id)
```

---

## 6. Billing & Pricing Engine

### 6.1 Pricing Strategy

The platform marks up domain costs from wholesale registrar pricing.

| TLD Category | Registrar Cost | Your Sell Price | Margin | Examples |
|---|---|---|---|---|
| Popular (.com, .net, .org) | $8-10 | $12-15 | ~50% | .com, .net, .org, .info |
| Country Code | $10-25 | $18-35 | ~40% | .co.uk, .de, .ca, .io |
| New gTLDs | $15-40 | $25-55 | ~40% | .dev, .app, .cloud, .tech |
| Premium | Variable | Variable + 30% | 30% | Short/dictionary words |

### 6.2 Billing Events

| Event | Trigger | Action |
|---|---|---|
| Registration | User purchases domain | Immediate charge, register with provider |
| Renewal | 30 days before expiry | Charge on file, renew with provider |
| Transfer In | User initiates transfer | Charge transfer fee, initiate with provider |
| Failed Renewal | Payment fails | 3 retry attempts over 7 days, then suspend |
| Refund | Within ICANN grace period (5 days) | Refund user, delete with provider |

### 6.3 Cron Jobs

| Job | Schedule | Description |
|---|---|---|
| `sync_domain_status` | Every 6 hours | Sync domain status/expiry from all providers |
| `process_renewals` | Daily at 00:00 UTC | Renew domains expiring in <30 days |
| `update_pricing` | Weekly (Sunday) | Fetch latest TLD pricing from all providers |
| `health_check` | Every 5 minutes | Ping all provider APIs, update circuit breakers |
| `cleanup_pending` | Every hour | Clean up stuck pending registrations |
| `send_expiry_notices` | Daily at 09:00 UTC | Email users about domains expiring in 30/7/1 days |

### 6.4 Pricing Sync Worker

```python
class PricingSyncWorker:
    """Weekly job to update TLD pricing from all providers."""

    async def run(self):
        for slug, adapter in self.adapters.items():
            try:
                pricing = await adapter.get_tld_pricing()
                await self.db.update_provider_pricing(slug, pricing)
                logger.info(f"Updated pricing for {slug}: {len(pricing)} TLDs")
            except Exception as e:
                logger.error(f"Failed to sync pricing for {slug}: {e}")

        # Update tld_routing with cheapest providers
        await self._optimize_routing()

    async def _optimize_routing(self):
        """Set preferred_provider to cheapest option per TLD."""
        all_tlds = await self.db.get_all_active_tlds()
        for tld in all_tlds:
            cheapest = await self.db.get_cheapest_provider(tld)
            second = await self.db.get_second_cheapest_provider(tld)
            await self.db.update_tld_routing(
                tld=tld,
                preferred_provider=cheapest.slug,
                fallback_provider=second.slug if second else None,
            )
```

---

## 7. Security Considerations

### 7.1 Credential Management

- **Provider API keys:** Stored encrypted at rest (AES-256-GCM) in `provider_configs.credentials`
- **Auth codes:** EPP transfer auth codes encrypted in `domains.auth_code`, decrypted only at transfer time
- **User sessions:** Standard JWT/session-based auth for platform API access
- **Webhook validation:** All provider webhooks verified via HMAC signature
- **Key rotation:** Provider API keys rotatable without downtime via dual-key support

### 7.2 Domain Security Features

- **Transfer Lock:** Enabled by default on all domains; users must explicitly unlock to transfer out
- **WHOIS Privacy:** Enabled by default where supported; registrant info replaced with proxy contact
- **DNSSEC:** Supported where provider and TLD allow; DS records managed via provider API
- **Rate Limiting:** Domain search endpoint rate-limited to 20 req/min per user to prevent enumeration
- **Abuse Prevention:** Integration with registrar fraud detection; suspicious registrations held for review
- **Audit Logging:** All domain operations logged with user, timestamp, IP, and provider details

### 7.3 ICANN Compliance

- Registrant email verification within 15 days of registration (ICANN requirement)
- WHOIS accuracy: must maintain valid registrant contact data
- Transfer policy: honor 60-day lock after registration/transfer per ICANN rules
- Data retention: maintain registration data per ICANN retention policies
- RAA compliance: ensure all registrations comply with Registrar Accreditation Agreement
- GDPR: registrant data subject to GDPR when applicable; WHOIS redaction for EU registrants

---

## 8. Implementation Roadmap

### Phase 1: Foundation (Weeks 1-3)

- [ ] Set up database schema (domains, dns_records, transactions, provider_configs, tld_routing)
- [ ] Implement `RegistrarAdapter` interface and `ProviderRouter`
- [ ] Build Name.com adapter (primary provider, modern REST API)
- [ ] Domain search and availability checking via platform API
- [ ] Basic domain registration flow with auto-DNS configuration
- [ ] Integration with platform billing system

### Phase 2: Core Features (Weeks 4-6)

- [ ] Build OpenSRS adapter (secondary provider, XML handling)
- [ ] Domain transfer (inbound) flow
- [ ] Full DNS management API and UI
- [ ] Auto-renewal system with billing integration
- [ ] WHOIS privacy toggle
- [ ] Domain management dashboard (list, detail, settings)
- [ ] SSL/TLS auto-provisioning via Let's Encrypt + DNS-01

### Phase 3: Advanced (Weeks 7-9)

- [ ] Build CentralNic Reseller adapter (third provider)
- [ ] Build Domain Name API adapter (fourth provider)
- [ ] Provider Router with cost optimization (cheapest-per-TLD auto-routing)
- [ ] Circuit breaker pattern and automatic failover
- [ ] TLD pricing sync cron job and pricing admin dashboard
- [ ] Expiry notification emails (30/7/1 day warnings)
- [ ] Domain Connect protocol support for one-click service linking

### Phase 4: Scale (Weeks 10-12)

- [ ] Migrate to self-hosted PowerDNS if >500 domains
- [ ] Bulk domain operations (multi-register, multi-transfer)
- [ ] Reseller/white-label sub-accounts
- [ ] Domain aftermarket (expired domain auctions)
- [ ] Advanced analytics (registration trends, revenue per TLD)
- [ ] API v2 with webhooks for domain events

---

## 9. Recommended Technology Stack

| Component | Technology | Rationale |
|---|---|---|
| API Server | Python (FastAPI) or Node.js (Hono) | FastAPI for async + type hints; Node if platform is JS-based |
| Database | PostgreSQL 16 | JSONB for provider configs, strong reliability |
| Cache | Redis | TLD pricing cache, rate limiting, session store |
| Task Queue | Celery (Python) or BullMQ (Node) | Async provider API calls, renewal processing |
| DNS (MVP) | Provider-hosted DNS via API | Zero infrastructure for DNS hosting |
| DNS (Scale) | PowerDNS + PostgreSQL backend | Self-hosted authoritative DNS |
| SSL | Certbot + ACME DNS-01 | Automated certificate provisioning |
| Monitoring | Prometheus + Grafana | Provider health, registration metrics |
| Secrets | HashiCorp Vault or SOPS | Provider API key encryption at rest |
| Email | Resend or AWS SES | Expiry notices, verification emails |

**Deployment:** All components run on the Hetzner dedicated server (Xeon W-2295, 320GB RAM, 2x1.92TB NVMe). The domain module runs as a service within the Coolify/K3s setup alongside the rest of the platform.

---

## 10. Cost Analysis

### Fixed Costs

| Item | Monthly Cost | Notes |
|---|---|---|
| Registrar accounts | $0 | All providers are free to join |
| Server (already provisioned) | $0 additional | Runs on existing Hetzner server |
| DNS hosting (MVP) | $0 | Included with registrar domain purchases |
| PowerDNS (at scale) | ~$10 | Hetzner Cloud VPS for secondary DNS |

### Variable Costs & Revenue (per 100 .com domains)

| Metric | Amount | Notes |
|---|---|---|
| Avg. wholesale cost | ~$8.50/yr | Across providers |
| Avg. sell price | $13.00/yr | Platform price to customer |
| Gross margin per domain | $4.50/yr | Per domain |
| Revenue (100 domains) | $1,300/yr | Registration year |
| Renewal revenue | $1,300/yr | Recurring annually |
| Wholesale cost | $850/yr | Cost to registrars |
| Net margin | $450/yr | Before payment processing fees |

### Revenue Projections

| Domains | Year 1 Revenue | Year 2 Revenue | Notes |
|---|---|---|---|
| 100 | $1,300 | $2,600 | Year 2 includes renewals |
| 500 | $6,500 | $13,000 | Consider PowerDNS migration |
| 1,000 | $13,000 | $26,000 | Volume discounts kick in |
| 5,000 | $65,000 | $130,000 | Premium reseller tier pricing |

> **Note:** Domain reselling margins are thin but valuable for customer retention. The real value is keeping customers on your platform by providing an all-in-one experience (hosting + domains + databases + SSL), reducing churn significantly.

---

*End of Specification*
