# DX Improvements (draft)
The service should have built in documentation accessible using "/docs" and "/docs/{method}". When integrating this service, users should be able to have emidiate access to documentation, it should not be nessesary having to access the documentation online, it should be accessible by simply accessing a URL.

## Curl
All payloads should be easy to copy and use. When curl is used to document an endpoint, the payload should be shown but without the curl command.

```json
{
    "carrier": "postnord",
    "shipment": {
      "sender": {
        "name": "Unisport Group",
        "street": "Industrivej",
        "houseNumber": "10",
        "city": "Copenhagen",
        "postalCode": "2300",
        "country": "DK",
        "phone": "+4512345678",
        "email": "logistics@unisport.dk"
      },
      "receiver": {
        "name": "Anna Svensson",
        "street": "Storgatan",
        "houseNumber": "1",
        "city": "Stockholm",
        "postalCode": "11122",
        "country": "SE",
        "phone": "+46701234567",
        "email": "anna@example.com"
      },
      "deliveryType": "home",
      "totalWeight": 2.5,
      "colli": [
        {
          "id": "box-001",
          "weight": 2.5,
          "dimensions": { "length": 30, "width": 20, "height": 10 },
          "items": [
            { "description": "Football boots", "weight": 0.8, "quantity": 1, "value": 129.95 }
          ]
        }
      ]
    },
    "idempotencyKey": "order-98765"
  }
```
This way all payloads can be used using curl or similar tools.
```
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d @payload.json
```
## Explain terminology
Freight terminology is difficult to understand, terms like COD and POD should be explained similar to how curl --help works in the terminal.
