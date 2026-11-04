package main

import "strings"

// The files the coding agents write. They are real programs: the API serves
// requests and the page calls it.

// serverPy is the orders API. Without described, it is the API as an agent
// that forgot the rule would write it: no description at /openapi.json.
func serverPy(described bool) string {
	text := serverTemplate
	if described {
		text = strings.Replace(text, "# (no description of the API yet)\n", serverDescription, 1)
		text = strings.Replace(text, "        # (the description is not served yet)\n", serverDescriptionRoute, 1)
	}

	return text
}

const serverTemplate = `"""Orders API for the shop. Standard library only."""
import json
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs

ADMIN_TOKEN = "shop-admin"

PRODUCTS = [
    {"id": 1, "name": "Mug", "price_cents": 1200},
    {"id": 2, "name": "Notebook", "price_cents": 800},
    {"id": 3, "name": "Pen", "price_cents": 300},
]
ORDERS = []

# (no description of the API yet)

class Handler(BaseHTTPRequestHandler):
    def reply(self, status, body):
        raw = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        path, _, query = self.path.partition("?")
        params = parse_qs(query)

        # (the description is not served yet)
        if path == "/api/health":
            return self.reply(200, {"ok": True, "database": os.environ.get("DATABASE_HOST", "")})

        if path == "/api/products":
            products = PRODUCTS
            if "max_price" in params:
                try:
                    limit = int(params["max_price"][0])
                except ValueError:
                    return self.reply(400, {"error": "max_price must be a whole number of cents"})
                products = [p for p in PRODUCTS if p["price_cents"] <= limit]
            return self.reply(200, products)

        found = re.fullmatch(r"/api/products/(\d+)", path)
        if found:
            for product in PRODUCTS:
                if product["id"] == int(found.group(1)):
                    return self.reply(200, product)
            return self.reply(404, {"error": "no such product"})

        if path == "/api/orders":
            if self.headers.get("Authorization") != "Bearer " + ADMIN_TOKEN:
                return self.reply(401, {"error": "the admin token is needed to list orders"})
            return self.reply(200, ORDERS)

        self.reply(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/api/orders":
            return self.reply(404, {"error": "not found"})

        try:
            length = int(self.headers.get("Content-Length") or 0)
            body = json.loads(self.rfile.read(length) or b"{}")
            product_id, quantity = int(body["product_id"]), int(body["quantity"])
        except (ValueError, KeyError, TypeError):
            return self.reply(400, {"error": "send product_id and quantity, both whole numbers"})

        product = next((p for p in PRODUCTS if p["id"] == product_id), None)
        if product is None:
            return self.reply(404, {"error": "no such product"})
        if quantity < 1:
            return self.reply(400, {"error": "quantity must be at least 1"})

        order = {
            "id": len(ORDERS) + 1,
            "product_id": product_id,
            "quantity": quantity,
            "total_cents": product["price_cents"] * quantity,
        }
        ORDERS.append(order)
        self.reply(201, order)

    def log_message(self, fmt, *args):
        print(self.address_string(), fmt % args, flush=True)


if __name__ == "__main__":
    print("orders API listening on 0.0.0.0:8000", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
`

const serverDescriptionRoute = `        if path == "/openapi.json":
            return self.reply(200, OPENAPI)

`

const serverDescription = `PRODUCT = {"$ref": "#/components/schemas/Product"}
ORDER = {"$ref": "#/components/schemas/Order"}
ERROR = {"description": "What was wrong", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Error"}}}}


def returns(description, schema):
    return {"description": description, "content": {"application/json": {"schema": schema}}}


# The description of this API, served at /openapi.json. Keep it true to the
# routes below.
OPENAPI = {
    "openapi": "3.0.3",
    "info": {
        "title": "Orders API",
        "version": "1.0.0",
        "description": "The shop's catalogue and its orders.",
    },
    "tags": [
        {"name": "Catalogue", "description": "What the shop sells."},
        {"name": "Orders", "description": "Placing orders, and listing them."},
        {"name": "Service", "description": "Whether the API is up."},
    ],
    "paths": {
        "/api/health": {
            "get": {
                "tags": ["Service"],
                "summary": "Whether the API is up",
                "responses": {"200": returns("It is up", {"$ref": "#/components/schemas/Health"})},
            }
        },
        "/api/products": {
            "get": {
                "tags": ["Catalogue"],
                "summary": "List the products",
                "parameters": [
                    {
                        "name": "max_price",
                        "in": "query",
                        "required": False,
                        "description": "Only products costing at most this many cents.",
                        "schema": {"type": "integer", "example": 1000},
                    }
                ],
                "responses": {
                    "200": returns("The products", {"type": "array", "items": PRODUCT}),
                    "400": ERROR,
                },
            }
        },
        "/api/products/{id}": {
            "get": {
                "tags": ["Catalogue"],
                "summary": "One product",
                "parameters": [
                    {
                        "name": "id",
                        "in": "path",
                        "required": True,
                        "description": "The product's number.",
                        "schema": {"type": "integer", "example": 2},
                    }
                ],
                "responses": {"200": returns("The product", PRODUCT), "404": ERROR},
            }
        },
        "/api/orders": {
            "get": {
                "tags": ["Orders"],
                "summary": "List the orders",
                "description": "For the shop's owner: needs the admin token.",
                "security": [{"adminToken": []}],
                "responses": {
                    "200": returns("The orders", {"type": "array", "items": ORDER}),
                    "401": ERROR,
                },
            },
            "post": {
                "tags": ["Orders"],
                "summary": "Place an order",
                "requestBody": {
                    "required": True,
                    "content": {"application/json": {"schema": {"$ref": "#/components/schemas/NewOrder"}}},
                },
                "responses": {"201": returns("The order as taken", ORDER), "400": ERROR, "404": ERROR},
            },
        },
    },
    "components": {
        "securitySchemes": {"adminToken": {"type": "http", "scheme": "bearer"}},
        "schemas": {
            "Health": {
                "type": "object",
                "properties": {"ok": {"type": "boolean"}, "database": {"type": "string"}},
            },
            "Product": {
                "type": "object",
                "required": ["id", "name", "price_cents"],
                "properties": {
                    "id": {"type": "integer", "example": 1},
                    "name": {"type": "string", "example": "Mug"},
                    "price_cents": {"type": "integer", "example": 1200},
                },
            },
            "NewOrder": {
                "type": "object",
                "required": ["product_id", "quantity"],
                "properties": {
                    "product_id": {"type": "integer", "example": 1},
                    "quantity": {"type": "integer", "example": 2},
                },
            },
            "Order": {
                "type": "object",
                "properties": {
                    "id": {"type": "integer", "example": 1},
                    "product_id": {"type": "integer", "example": 1},
                    "quantity": {"type": "integer", "example": 2},
                    "total_cents": {"type": "integer", "example": 2400},
                },
            },
            "Error": {"type": "object", "properties": {"error": {"type": "string"}}},
        },
    },
}

`

// indexHTML is the storefront. It calls /api on its own address; the
// platform passes that on to the API's machine.
const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>The shop</title>
  <style>
    body { font: 16px/1.5 system-ui, sans-serif; max-width: 34rem; margin: 3rem auto; padding: 0 1rem; color: #1f2328; }
    h1 { font-size: 1.5rem; }
    ul { list-style: none; padding: 0; }
    li { display: flex; justify-content: space-between; align-items: center; gap: 1rem; padding: .6rem 0; border-bottom: 1px solid #d8dee4; }
    button { font: inherit; padding: .25rem .75rem; cursor: pointer; }
    #said { min-height: 1.5rem; color: #1a7f37; }
  </style>
</head>
<body>
  <h1>The shop</h1>
  <ul id="products"><li>Loading the products</li></ul>
  <p id="said" role="status"></p>

  <script>
    const list = document.getElementById("products");
    const said = document.getElementById("said");

    function price(cents) {
      return "$" + (cents / 100).toFixed(2);
    }

    async function order(product) {
      const res = await fetch("/api/orders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ product_id: product.id, quantity: 1 }),
      });
      const taken = await res.json();
      said.textContent = res.ok
        ? "Order " + taken.id + " taken: " + product.name + ", " + price(taken.total_cents)
        : "That did not work: " + taken.error;
    }

    async function load() {
      const res = await fetch("/api/products");
      const products = await res.json();
      list.textContent = "";
      for (const product of products) {
        const item = document.createElement("li");
        const name = document.createElement("span");
        name.textContent = product.name + " " + price(product.price_cents);
        const button = document.createElement("button");
        button.textContent = "Order " + product.name;
        button.addEventListener("click", () => order(product));
        item.append(name, button);
        list.append(item);
      }
    }

    load().catch((err) => {
      list.textContent = "The products could not be loaded: " + err;
    });
  </script>
</body>
</html>
`
