// Package ubl is a basic implementation to create a UBL Invoice.
//
// This is needed for Peppol: https://docs.peppol.eu/poacc/billing/3.0/
// Specification: https://docs.peppol.eu/poacc/billing/3.0/syntax/ubl-invoice/tree/
// The result can be validated with the validate package.
package ubl

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Invoice struct {
	xml                   *xmlInvoice
	ID                    string
	CustomizationID       string
	ProfileID             string
	SupplierName          string
	SupplierVat           string
	SupplierPeppolID      string
	SupplierAddress       Address
	CustomerName          string
	CustomerVat           string
	CustomerPeppolID      string
	CustomerAddress       Address
	DeliveryAddress       *Address   // Optional: required for intra-community supply (BT-80)
	ActualDeliveryDate    *time.Time // Optional: required for intra-community supply (BT-72)
	InvoicePeriodStart    *time.Time // Optional: alternative to delivery date for IC supply (BG-14)
	InvoicePeriodEnd      *time.Time // Optional: alternative to delivery date for IC supply (BG-14)
	Iban                  string
	Bic                   string
	Note                  string
	Lines                 []InvoiceLine
	PdfInvoiceFilename    string
	PdfInvoiceData        string
	PdfInvoiceDescription string
}

type InvoiceLine struct {
	Quantity           float64
	Price              float64
	TaxPercentage      float64
	TaxCategoryID      string
	TaxCategoryName    string
	TaxExemptionReason string // Optional: required for category K (BT-120/121)
	TaxExemptionCode   string // Optional: exemption reason code (BT-121)

	Name        string
	Description string
}

type taxKey struct {
	Rate       float64
	CategoryID string
}

type Address struct {
	StreetName  string
	CityName    string
	PostalZone  string
	CountryCode string
}

// cleanVATIdentifier ensures VAT identifier has proper ISO 3166-1 alpha-2 country prefix
func cleanVATIdentifier(vatID, countryCode string) string {
	// Remove any leading numeric scheme identifiers (e.g., "9925")
	// VAT IDs should start with letters (country code)
	for len(vatID) > 0 && vatID[0] >= '0' && vatID[0] <= '9' {
		vatID = vatID[1:]
	}

	// If VAT ID doesn't start with country code, prepend it
	if len(vatID) >= 2 && (vatID[0] < 'A' || vatID[0] > 'Z') {
		return countryCode + vatID
	}

	// Extract the country prefix from VAT ID (first 2 letters)
	if len(vatID) < 2 {
		return countryCode + vatID
	}

	vatPrefix := vatID[0:2]

	// Special case: Convert GR to EL for Greece
	if vatPrefix == "GR" {
		return "EL" + vatID[2:]
	}

	return vatID
}

func (inv *Invoice) Generate() ([]byte, error) {
	inv.xml = &xmlInvoice{
		Xmlns:            "urn:oasis:names:specification:ubl:schema:xsd:Invoice-2",
		Cac:              "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2",
		Cbc:              "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2",
		CustomizationID:  inv.CustomizationID,
		ProfileID:        inv.ProfileID,
		IssueDate:        time.Now().Format("2006-01-02"),
		DueDate:          time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		InvoiceTypeCode:  "380",
		DocumentCurrency: "EUR",
		ID:               inv.ID,
		OrderReference:   inv.ID,
	}

	// Clean and validate VAT identifiers
	supplierVat := cleanVATIdentifier(inv.SupplierVat, inv.SupplierAddress.CountryCode)
	customerVat := cleanVATIdentifier(inv.CustomerVat, inv.CustomerAddress.CountryCode)

	inv.xml.SupplierParty = xmlSupplierParty{
		Party: xmlParty{
			EndpointID: xmlEndpointID{
				Value:    inv.SupplierPeppolID[5:],
				SchemeID: inv.SupplierPeppolID[0:4],
			},
			PartyName:        inv.SupplierName,
			RegistrationName: inv.SupplierName,
			PartyTaxScheme: xmlPartyTaxScheme{
				CompanyID: supplierVat,
				TaxScheme: xmlTaxScheme{
					ID: "VAT",
				},
			},
		},
	}

	inv.xml.SupplierParty.Party.PostalAddress = xmlPostalAddress{
		StreetName: inv.SupplierAddress.StreetName,
		CityName:   inv.SupplierAddress.CityName,
		PostalZone: inv.SupplierAddress.PostalZone,
		Country:    xmlCountry{IdentificationCode: inv.SupplierAddress.CountryCode},
	}

	inv.xml.CustomerParty.Party.PostalAddress = xmlPostalAddress{
		StreetName: inv.CustomerAddress.StreetName,
		CityName:   inv.CustomerAddress.CityName,
		PostalZone: inv.CustomerAddress.PostalZone,
		Country:    xmlCountry{IdentificationCode: inv.CustomerAddress.CountryCode},
	}

	inv.xml.CustomerParty = xmlCustomerParty{
		Party: xmlParty{
			EndpointID: xmlEndpointID{
				Value:    inv.CustomerPeppolID[5:],
				SchemeID: inv.CustomerPeppolID[0:4],
			},
			PartyName:        inv.CustomerName,
			RegistrationName: inv.CustomerName,
			PartyTaxScheme: xmlPartyTaxScheme{
				CompanyID: customerVat,
				TaxScheme: xmlTaxScheme{
					ID: "VAT",
				},
			},
			PostalAddress: xmlPostalAddress{
				Country: xmlCountry{
					IdentificationCode: inv.CustomerAddress.CountryCode,
				},
			},
		},
	}

	// Add delivery information if provided (required for intra-community supply)
	if inv.DeliveryAddress != nil {
		inv.xml.Delivery = &xmlDelivery{
			ActualDeliveryDate: "",
			DeliveryLocation: xmlDeliveryLocation{
				Address: xmlPostalAddress{
					StreetName: inv.DeliveryAddress.StreetName,
					CityName:   inv.DeliveryAddress.CityName,
					PostalZone: inv.DeliveryAddress.PostalZone,
					Country: xmlCountry{
						IdentificationCode: inv.DeliveryAddress.CountryCode,
					},
				},
			},
		}
	}

	// Add actual delivery date if provided
	if inv.ActualDeliveryDate != nil {
		if inv.xml.Delivery == nil {
			inv.xml.Delivery = &xmlDelivery{}
		}
		inv.xml.Delivery.ActualDeliveryDate = inv.ActualDeliveryDate.Format("2006-01-02")
	}

	// Add invoicing period if provided (alternative to delivery date)
	if inv.InvoicePeriodStart != nil && inv.InvoicePeriodEnd != nil {
		inv.xml.InvoicePeriod = &xmlInvoicePeriod{
			StartDate: inv.InvoicePeriodStart.Format("2006-01-02"),
			EndDate:   inv.InvoicePeriodEnd.Format("2006-01-02"),
		}
	}

	inv.xml.PaymentMeans = xmlPaymentMeans{
		PaymentMeansCode: "1",
		PayeeFinancialAccount: xmlFinancialAccount{
			ID: inv.Iban,
			FinancialInstitutionBranch: xmlFinancialInstitutionBranch{
				ID: inv.Bic,
			},
		},
	}

	// Only include PaymentTerms if Note is not empty
	if inv.Note != "" {
		inv.xml.PaymentTerms = xmlPaymentTerms{
			Note: inv.Note,
		}
	}

	inv.addLines()

	if inv.PdfInvoiceFilename != "" && inv.PdfInvoiceData == "" {
		err := inv.addAttachmentFromFile(inv.PdfInvoiceFilename, "Invoice")
		if err != nil {
			return nil, fmt.Errorf("add attachment failed: %w", err)
		}
	}
	if inv.PdfInvoiceData != "" {
		err := inv.addAttachmentFromData(inv.PdfInvoiceData, "application/pdf", inv.PdfInvoiceFilename, inv.PdfInvoiceDescription)
		if err != nil {
			return nil, fmt.Errorf("add attachment from data: %w", err)
		}
	}
	output, err := xml.MarshalIndent(inv.xml, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("xml marshal failed: %w", err)
	}
	return []byte(xml.Header + string(output)), nil
}

func (inv *Invoice) addAttachmentFromFile(filename, description string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	mime := http.DetectContentType(data)
	// using base64 encoding for the embedded binary content
	encoded := base64.StdEncoding.EncodeToString(data)

	return inv.addAttachmentFromData(encoded, mime, filename, description)
}

func (inv *Invoice) addAttachmentFromData(encodedData, mime, filename, description string) error {
	inv.xml.AdditionalDocumentReference = []xmlDocumentReference{
		{
			ID:                  "UBL.BE",
			DocumentDescription: "CommercialInvoice",
		},
		{
			ID:                  inv.ID,
			DocumentDescription: description,
			Attachment: []xmlAttachment{
				{xmlEmbeddedDocumentBinaryObject{
					Value:    encodedData,
					MimeCode: mime,
					Filename: filename,
				}},
			},
		},
	}

	return nil
}

func round(amount float64) float64 {
	return math.Round(amount*100) / 100
}

type taxSummary struct {
	key     taxKey
	taxable float64
	tax     float64
	catName string
}

func calculateTaxTotals(lines []InvoiceLine) (lineTotal float64, taxTotal float64, subtotals []xmlTaxSubtotal) {
	summaries := make(map[taxKey]*taxSummary)

	for _, line := range lines {
		lineAmount := round(line.Quantity * line.Price)

		// Default to "S" (Standard rated) if not specified
		categoryID := line.TaxCategoryID
		if categoryID == "" {
			categoryID = "S"
		}
		categoryName := line.TaxCategoryName
		if categoryName == "" {
			categoryName = "Standard rated"
		}

		// For intra-community supply (K), enforce 0% tax rate
		taxRate := line.TaxPercentage
		if categoryID == "K" {
			taxRate = 0
		}

		tax := round(lineAmount * taxRate / 100)

		key := taxKey{Rate: taxRate, CategoryID: categoryID}

		lineTotal += lineAmount
		taxTotal += tax

		if summaries[key] == nil {
			summaries[key] = &taxSummary{key: key, catName: categoryName}
		}
		summaries[key].taxable += lineAmount
		summaries[key].tax += tax
	}

	for _, summary := range summaries {
		taxCat := xmlTaxCategory{
			ID:        summary.key.CategoryID,
			Name:      summary.catName,
			Percent:   summary.key.Rate,
			TaxScheme: xmlTaxScheme{ID: "VAT"},
		}

		// For intra-community supply (K), add exemption reason code
		if summary.key.CategoryID == "K" {
			taxCat.TaxExemptionReasonCode = "VATEX-EU-IC"
			taxCat.TaxExemptionReason = "Intra-community supply"
		}

		subtotals = append(subtotals, xmlTaxSubtotal{
			TaxableAmount: xmlAmount{Value: summary.taxable, CurrencyID: "EUR"},
			TaxAmount:     xmlAmount{Value: summary.tax, CurrencyID: "EUR"},
			TaxCategory:   taxCat,
		})
	}

	return
}

func (inv *Invoice) addLines() {
	for i, line := range inv.Lines {
		lineAmount := round(line.Quantity * line.Price)
		tax := round(lineAmount * line.TaxPercentage / 100)

		// Default to "S" (Standard rated) if not specified
		categoryID := line.TaxCategoryID
		if categoryID == "" {
			categoryID = "S"
		}
		categoryName := line.TaxCategoryName
		if categoryName == "" {
			categoryName = "Standard rated"
		}

		// For intra-community supply (K), enforce 0% tax rate
		taxRate := line.TaxPercentage
		if categoryID == "K" {
			taxRate = 0
			tax = 0
		}

		taxCat := xmlTaxCategory{
			ID:        categoryID,
			Name:      categoryName,
			Percent:   taxRate,
			TaxScheme: xmlTaxScheme{ID: "VAT"},
		}

		// Add exemption reason for intra-community supply
		if categoryID == "K" {
			if line.TaxExemptionCode != "" {
				taxCat.TaxExemptionReasonCode = line.TaxExemptionCode
			} else {
				taxCat.TaxExemptionReasonCode = "VATEX-EU-IC"
			}
			if line.TaxExemptionReason != "" {
				taxCat.TaxExemptionReason = line.TaxExemptionReason
			} else {
				taxCat.TaxExemptionReason = "Intra-community supply"
			}
		}

		inv.xml.InvoiceLines = append(inv.xml.InvoiceLines, xmlInvoiceLine{
			ID:                  strconv.Itoa(i + 1),
			InvoicedQuantity:    xmlQuantity{Value: line.Quantity, UnitCode: "ZZ"},
			LineExtensionAmount: xmlAmount{Value: lineAmount, CurrencyID: "EUR"},
			TaxTotal:            xmlTaxTotal{TaxAmount: xmlAmount{Value: tax, CurrencyID: "EUR"}},
			Item: xmlItem{
				Name:                  line.Name,
				Description:           line.Description,
				ClassifiedTaxCategory: taxCat,
			},
			Price: xmlPrice{PriceAmount: xmlAmount{Value: line.Price, CurrencyID: "EUR"}},
		})
	}

	lineTotal, taxTotal, subtotals := calculateTaxTotals(inv.Lines)
	total := round(lineTotal + taxTotal)

	inv.xml.TaxTotal = xmlTaxTotal{
		TaxAmount:   xmlAmount{Value: taxTotal, CurrencyID: "EUR"},
		TaxSubtotal: subtotals,
	}

	inv.xml.LegalMonetaryTotal = xmlMonetaryTotal{
		LineExtensionAmount: xmlAmount{Value: lineTotal, CurrencyID: "EUR"},
		TaxExclusiveAmount:  xmlAmount{Value: lineTotal, CurrencyID: "EUR"},
		TaxInclusiveAmount:  xmlAmount{Value: total, CurrencyID: "EUR"},
		PayableAmount:       xmlAmount{Value: total, CurrencyID: "EUR"},
	}
}

type CreditNote struct {
	xml                      *xmlCreditNote
	ID                       string
	CustomizationID          string
	ProfileID                string
	SupplierName             string
	SupplierVat              string
	SupplierPeppolID         string
	SupplierAddress          Address
	CustomerName             string
	CustomerVat              string
	CustomerPeppolID         string
	CustomerAddress          Address
	DeliveryAddress          *Address   // Optional: required for intra-community supply (BT-80)
	ActualDeliveryDate       *time.Time // Optional: required for intra-community supply (BT-72)
	InvoicePeriodStart       *time.Time // Optional: alternative to delivery date for IC supply (BG-14)
	InvoicePeriodEnd         *time.Time // Optional: alternative to delivery date for IC supply (BG-14)
	Iban                     string
	Bic                      string
	Note                     string
	Lines                    []InvoiceLine
	PdfCreditNoteFilename    string
	PdfCreditNoteData        string
	PdfCreditNoteDescription string
}

type xmlCreditNote struct {
	XMLName                     xml.Name               `xml:"CreditNote"`
	Xmlns                       string                 `xml:"xmlns,attr"`
	Cac                         string                 `xml:"xmlns:cac,attr"`
	Cbc                         string                 `xml:"xmlns:cbc,attr"`
	CustomizationID             string                 `xml:"cbc:CustomizationID"`
	ProfileID                   string                 `xml:"cbc:ProfileID"`
	ID                          string                 `xml:"cbc:ID"`
	IssueDate                   string                 `xml:"cbc:IssueDate"`
	CreditNoteTypeCode          string                 `xml:"cbc:CreditNoteTypeCode"`
	DocumentCurrency            string                 `xml:"cbc:DocumentCurrencyCode"`
	InvoicePeriod               *xmlInvoicePeriod      `xml:"cac:InvoicePeriod,omitempty"`
	OrderReference              string                 `xml:"cac:OrderReference>cbc:ID"`
	AdditionalDocumentReference []xmlDocumentReference `xml:"cac:AdditionalDocumentReference,omitempty"`
	SupplierParty               xmlSupplierParty       `xml:"cac:AccountingSupplierParty"`
	CustomerParty               xmlCustomerParty       `xml:"cac:AccountingCustomerParty"`
	Delivery                    *xmlDelivery           `xml:"cac:Delivery,omitempty"`
	PaymentMeans                xmlPaymentMeans        `xml:"cac:PaymentMeans"`
	PaymentTerms                xmlPaymentTerms        `xml:"cac:PaymentTerms,omitempty"`
	TaxTotal                    xmlTaxTotal            `xml:"cac:TaxTotal"`
	LegalMonetaryTotal          xmlMonetaryTotal       `xml:"cac:LegalMonetaryTotal"`
	CreditNoteLines             []xmlCreditNoteLine    `xml:"cac:CreditNoteLine"`
}

type xmlCreditNoteLine struct {
	ID                  string      `xml:"cbc:ID"`
	CreditedQuantity    xmlQuantity `xml:"cbc:CreditedQuantity"`
	LineExtensionAmount xmlAmount   `xml:"cbc:LineExtensionAmount"`
	Item                xmlItem     `xml:"cac:Item"`
	Price               xmlPrice    `xml:"cac:Price"`
}

func (cn *CreditNote) GenerateCreditNote() ([]byte, error) {
	cn.xml = &xmlCreditNote{
		Xmlns:              "urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2",
		Cac:                "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2",
		Cbc:                "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2",
		CustomizationID:    cn.CustomizationID,
		ProfileID:          cn.ProfileID,
		ID:                 cn.ID,
		IssueDate:          time.Now().Format("2006-01-02"),
		CreditNoteTypeCode: "381",
		DocumentCurrency:   "EUR",
		OrderReference:     cn.ID,
	}

	// Clean and validate VAT identifiers
	supplierVat := cleanVATIdentifier(cn.SupplierVat, cn.SupplierAddress.CountryCode)
	customerVat := cleanVATIdentifier(cn.CustomerVat, cn.CustomerAddress.CountryCode)

	cn.xml.SupplierParty = xmlSupplierParty{
		Party: xmlParty{
			EndpointID: xmlEndpointID{
				Value:    cn.SupplierPeppolID[5:],
				SchemeID: cn.SupplierPeppolID[0:4],
			},
			PartyName:        cn.SupplierName,
			RegistrationName: cn.SupplierName,
			PartyTaxScheme: xmlPartyTaxScheme{
				CompanyID: supplierVat,
				TaxScheme: xmlTaxScheme{
					ID: "VAT",
				},
			},
		},
	}

	cn.xml.SupplierParty.Party.PostalAddress = xmlPostalAddress{
		StreetName: cn.SupplierAddress.StreetName,
		CityName:   cn.SupplierAddress.CityName,
		PostalZone: cn.SupplierAddress.PostalZone,
		Country:    xmlCountry{IdentificationCode: cn.SupplierAddress.CountryCode},
	}

	cn.xml.CustomerParty.Party.PostalAddress = xmlPostalAddress{
		StreetName: cn.CustomerAddress.StreetName,
		CityName:   cn.CustomerAddress.CityName,
		PostalZone: cn.CustomerAddress.PostalZone,
		Country:    xmlCountry{IdentificationCode: cn.CustomerAddress.CountryCode},
	}

	cn.xml.CustomerParty = xmlCustomerParty{
		Party: xmlParty{
			EndpointID: xmlEndpointID{
				Value:    cn.CustomerPeppolID[5:],
				SchemeID: cn.CustomerPeppolID[0:4],
			},
			PartyName:        cn.CustomerName,
			RegistrationName: cn.CustomerName,
			PartyTaxScheme: xmlPartyTaxScheme{
				CompanyID: customerVat,
				TaxScheme: xmlTaxScheme{
					ID: "VAT",
				},
			},
			PostalAddress: xmlPostalAddress{
				Country: xmlCountry{
					IdentificationCode: cn.CustomerAddress.CountryCode,
				},
			},
		},
	}

	// Add delivery information if provided (required for intra-community supply)
	if cn.DeliveryAddress != nil {
		cn.xml.Delivery = &xmlDelivery{
			ActualDeliveryDate: "",
			DeliveryLocation: xmlDeliveryLocation{
				Address: xmlPostalAddress{
					StreetName: cn.DeliveryAddress.StreetName,
					CityName:   cn.DeliveryAddress.CityName,
					PostalZone: cn.DeliveryAddress.PostalZone,
					Country: xmlCountry{
						IdentificationCode: cn.DeliveryAddress.CountryCode,
					},
				},
			},
		}
	}

	// Add actual delivery date if provided
	if cn.ActualDeliveryDate != nil {
		if cn.xml.Delivery == nil {
			cn.xml.Delivery = &xmlDelivery{}
		}
		cn.xml.Delivery.ActualDeliveryDate = cn.ActualDeliveryDate.Format("2006-01-02")
	}

	// Add invoicing period if provided (alternative to delivery date)
	if cn.InvoicePeriodStart != nil && cn.InvoicePeriodEnd != nil {
		cn.xml.InvoicePeriod = &xmlInvoicePeriod{
			StartDate: cn.InvoicePeriodStart.Format("2006-01-02"),
			EndDate:   cn.InvoicePeriodEnd.Format("2006-01-02"),
		}
	}

	cn.xml.PaymentMeans = xmlPaymentMeans{
		PaymentMeansCode: "1",
		PayeeFinancialAccount: xmlFinancialAccount{
			ID: cn.Iban,
			FinancialInstitutionBranch: xmlFinancialInstitutionBranch{
				ID: cn.Bic,
			},
		},
	}

	// Ensure PaymentTerms is only included if Note is not empty
	if cn.Note != "" {
		cn.xml.PaymentTerms = xmlPaymentTerms{
			Note: cn.Note,
		}
	}

	cn.addLines()

	if cn.PdfCreditNoteFilename != "" && cn.PdfCreditNoteData == "" {
		err := cn.addAttachmentFromFile(cn.PdfCreditNoteFilename, "CreditNote")
		if err != nil {
			return nil, fmt.Errorf("add attachment failed: %w", err)
		}
	}
	if cn.PdfCreditNoteData != "" {
		err := cn.addAttachmentFromData(cn.PdfCreditNoteData, "application/pdf", cn.PdfCreditNoteFilename, cn.PdfCreditNoteDescription)
		if err != nil {
			return nil, fmt.Errorf("add attachment from data: %w", err)
		}
	}
	output, err := xml.MarshalIndent(cn.xml, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("xml marshal failed: %w", err)
	}
	return []byte(xml.Header + string(output)), nil
}

func (cn *CreditNote) addAttachmentFromFile(filename, description string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	mime := http.DetectContentType(data)
	// using base64 encoding for the embedded binary content
	encoded := base64.StdEncoding.EncodeToString(data)

	return cn.addAttachmentFromData(encoded, mime, filename, description)
}

func (cn *CreditNote) addAttachmentFromData(encodedData, mime, filename, description string) error {
	cn.xml.AdditionalDocumentReference = []xmlDocumentReference{
		{
			ID:                  "UBL.BE",
			DocumentDescription: "CommercialInvoice",
		},
		{
			ID:                  cn.ID,
			DocumentDescription: description,
			Attachment: []xmlAttachment{
				{xmlEmbeddedDocumentBinaryObject{
					Value:    encodedData,
					MimeCode: mime,
					Filename: filename,
				}},
			},
		},
	}

	return nil
}

func (cn *CreditNote) addLines() {
	for i, line := range cn.Lines {
		lineAmount := round(line.Quantity * line.Price)

		// Default to "S" (Standard rated) if not specified
		categoryID := line.TaxCategoryID
		if categoryID == "" {
			categoryID = "S"
		}
		categoryName := line.TaxCategoryName
		if categoryName == "" {
			categoryName = "Standard rated"
		}

		// For intra-community supply (K), enforce 0% tax rate
		taxRate := line.TaxPercentage
		if categoryID == "K" {
			taxRate = 0
		}

		taxCat := xmlTaxCategory{
			ID:        categoryID,
			Name:      categoryName,
			Percent:   taxRate,
			TaxScheme: xmlTaxScheme{ID: "VAT"},
		}

		// Add exemption reason for intra-community supply
		if categoryID == "K" {
			if line.TaxExemptionCode != "" {
				taxCat.TaxExemptionReasonCode = line.TaxExemptionCode
			} else {
				taxCat.TaxExemptionReasonCode = "VATEX-EU-IC"
			}
			if line.TaxExemptionReason != "" {
				taxCat.TaxExemptionReason = line.TaxExemptionReason
			} else {
				taxCat.TaxExemptionReason = "Intra-community supply"
			}
		}

		cn.xml.CreditNoteLines = append(cn.xml.CreditNoteLines, xmlCreditNoteLine{
			ID:                  strconv.Itoa(i + 1),
			CreditedQuantity:    xmlQuantity{Value: line.Quantity, UnitCode: "ZZ"},
			LineExtensionAmount: xmlAmount{Value: lineAmount, CurrencyID: "EUR"},
			Item: xmlItem{
				Name:                  line.Name,
				Description:           line.Description,
				ClassifiedTaxCategory: taxCat,
			},
			Price: xmlPrice{PriceAmount: xmlAmount{Value: line.Price, CurrencyID: "EUR"}},
		})
	}

	lineTotal, taxTotal, subtotals := calculateTaxTotals(cn.Lines)
	total := round(lineTotal + taxTotal)

	cn.xml.TaxTotal = xmlTaxTotal{
		TaxAmount:   xmlAmount{Value: taxTotal, CurrencyID: "EUR"},
		TaxSubtotal: subtotals,
	}

	cn.xml.LegalMonetaryTotal = xmlMonetaryTotal{
		LineExtensionAmount: xmlAmount{Value: lineTotal, CurrencyID: "EUR"},
		TaxExclusiveAmount:  xmlAmount{Value: lineTotal, CurrencyID: "EUR"},
		TaxInclusiveAmount:  xmlAmount{Value: total, CurrencyID: "EUR"},
		PayableAmount:       xmlAmount{Value: total, CurrencyID: "EUR"},
	}
}
