# lib-commons/jwt

Minimal JWT library for service-to-service authentication.

## Requirements

`JWT_SECRET` **must** be set in the environment. It is never optional.

Every service using this library must load the environment as the **very first import** in `main.go`:

```go
import (
    _ "github.com/joho/godotenv/autoload" // loads .env automatically — must be first
    // ... rest of imports
)
```

And provide a `.env` file (or inject the variable directly in the environment):

```env
JWT_SECRET=your-super-secret-key
```

## Usage

### Generate a token

```go
import jwtlib "lib-commons/src/jwt"

token, err := jwtlib.GenerateToken(jwtlib.Claims{
    UserId:      "user-uuid",
    TrackId:     "track-uuid",
    OrganizerId: "organizer-uuid",
    ActivityId:  "activity-uuid",
    IsAuth:      false,
})
```

### Verify a token

```go
claims, err := jwtlib.VerifyToken(token)
if err != nil {
    // jwtlib.ErrExpiredToken → token has expired
    // jwtlib.ErrInvalidToken → bad signature or malformed
}
// claims.UserId, claims.IsAuth, claims.Exp ...
```

## Claims

| Field         | Type     | Description                                              |
|---------------|----------|----------------------------------------------------------|
| `UserId`      | `string` | User UUID                                                |
| `TrackId`     | `string` | Track UUID                                               |
| `OrganizerId` | `string` | Organizer UUID                                           |
| `ActivityId`  | `string` | Activity UUID                                            |
| `IsAuth`      | `bool`   | Whether the token has already been verified downstream   |
| `Exp`         | `int64`  | Expiration timestamp (set automatically, 1 hour)         |
| `Iat`         | `int64`  | Issued-at timestamp (set automatically)                  |
