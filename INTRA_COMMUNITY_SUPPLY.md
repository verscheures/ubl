# Intra-Community Supply Support

## Overview

This package now supports PEPPOL-compliant intra-community supply invoices and credit notes (VAT category `K`). These documents require special handling according to PEPPOL BIS Billing 3.0 rules.

## PEPPOL Business Rules for Intra-Community Supply (Category K)

When using VAT category `K` (Intra-community supply), the following PEPPOL business rules must be satisfied:

1. **BR-IC-05**: The invoiced item VAT rate must be **0%**
2. **BR-IC-09**: The VAT category tax amount must be **0** (zero)
3. **BR-IC-10**: Must include VAT exemption reason code (recommended: `VATEX-EU-IC`) or exemption reason text
4. **BR-IC-11**: Must include either:
   - Actual delivery date (`ActualDeliveryDate`), OR
   - Invoicing period (`InvoicePeriodStart` and `InvoicePeriodEnd`)
5. **BR-IC-12**: Must include deliver to country code (`DeliveryAddress.CountryCode`)

## Implementation

The package now automatically:
- Enforces **0%** tax rate for category `K` invoices (overrides any specified rate)
- Sets tax amount to **0** for category `K` line items
- Adds default exemption reason code `VATEX-EU-IC` and reason text "Intra-community supply"
- Supports optional delivery address, delivery date, and invoicing period

## Usage Example

### Basic Intra-Community Invoice

```go
package main

import (
	"time"
	"github.com/verscheures/ubl"
)

func main() {
	// Delivery date for intra-community supply
	deliveryDate := time.Date(2025, 12, 10, 0, 0, 0, 0, time.UTC)
	
	inv := ubl.Invoice{
		ID:              "THX-F-0023",
		CustomizationID: "urn:cen.eu:en16931:2017#compliant#urn:fdc:peppol.eu:2017:poacc:billing:3.0",
		ProfileID:       "urn:fdc:peppol.eu:2017:poacc:billing:01:1.0",
		
		// Supplier (Belgium)
		SupplierName:     "Cynalco Communication",
		SupplierVat:      "BE0471959240",
		SupplierPeppolID: "0208:0418907663",
		SupplierAddress: ubl.Address{
			StreetName:  "Bosklapperstraat 15",
			CityName:    "Brussel",
			PostalZone:  "1000",
			CountryCode: "BE",
		},
		
		// Customer (Germany)
		CustomerName:     "Nen Duits",
		CustomerVat:      "DE812789807",
		CustomerPeppolID: "9930:de102929309",
		CustomerAddress: ubl.Address{
			CountryCode: "DE", // Customer country
		},
		
		// REQUIRED: Delivery address with country code (BR-IC-12)
		DeliveryAddress: &ubl.Address{
			CountryCode: "DE", // Destination country
		},
		
		// REQUIRED: Delivery date (BR-IC-11) - Alternative: use InvoicePeriod
		ActualDeliveryDate: &deliveryDate,
		
		Iban: "BE20738001803717",
		Bic:  "KREDBEBB",
		Note: "N/A",
		
		// Invoice line with category K
		Lines: []ubl.InvoiceLine{
			{
				Quantity:      1,
				Price:         20,
				TaxPercentage: 0, // Will be enforced as 0 for category K
				TaxCategoryID: "K",
				TaxCategoryName: "Intra-EU supply",
				// Optional: override default exemption reason
				// TaxExemptionCode: "VATEX-EU-IC",
				// TaxExemptionReason: "Intra-community supply",
				Name:        "intracomm cn",
				Description: "N/A",
			},
		},
	}
	
	xmlBytes, err := inv.Generate()
	if err != nil {
		panic(err)
	}
	
	// xmlBytes now contains PEPPOL-compliant intra-community invoice
}
```

### Using Invoicing Period Instead of Delivery Date

```go
startDate := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
endDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

inv := ubl.Invoice{
	// ... other fields ...
	
	// Use invoicing period instead of delivery date
	InvoicePeriodStart: &startDate,
	InvoicePeriodEnd:   &endDate,
	
	DeliveryAddress: &ubl.Address{
		CountryCode: "DE",
	},
	
	Lines: []ubl.InvoiceLine{
		{
			TaxCategoryID: "K",
			// ... other fields ...
		},
	},
}
```

## New Fields

### Invoice and CreditNote Structs

Both `Invoice` and `CreditNote` now support:

- `DeliveryAddress *Address`: Delivery location (required for category K - BT-80)
- `ActualDeliveryDate *time.Time`: Actual delivery date (required for category K - BT-72)
- `InvoicePeriodStart *time.Time`: Alternative to delivery date (BG-14)
- `InvoicePeriodEnd *time.Time`: Alternative to delivery date (BG-14)

### InvoiceLine Struct

Used by both Invoice and CreditNote:

- `TaxExemptionCode string`: VAT exemption reason code (default: "VATEX-EU-IC" for category K)
- `TaxExemptionReason string`: VAT exemption reason text (default: "Intra-community supply" for category K)

## Validation

The package automatically ensures:
- Tax rate is 0% for category K
- Tax amount is 0 for category K
- Exemption reason code and text are included for category K

You must manually ensure:
- Either `ActualDeliveryDate` OR both `InvoicePeriodStart` and `InvoicePeriodEnd` are provided
- `DeliveryAddress` with `CountryCode` is provided
- Customer VAT number is valid
- Supplier and customer are in different EU countries

## Common Validation Errors

### Before Fix
```
[BR-IC-05] - Invoiced item VAT rate must be 0 (zero)
[BR-IC-09] - VAT category tax amount must be 0 (zero)  
[BR-IC-10] - Must have VAT exemption reason code or text
[BR-IC-11] - Must have delivery date or invoicing period
[BR-IC-12] - Must have deliver to country code
```

### After Fix
All validation rules pass automatically when:
1. You set `TaxCategoryID = "K"`
2. You provide `DeliveryAddress.CountryCode`
3. You provide either `ActualDeliveryDate` or `InvoicePeriod*` dates

## Notes

- The package enforces 0% tax regardless of the `TaxPercentage` value specified in the line
- Default exemption reason code is `VATEX-EU-IC` (standard PEPPOL code)
- Default exemption reason text is "Intra-community supply" (English)
- You can override exemption reason code/text per line if needed
