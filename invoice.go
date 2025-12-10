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
	Iban                  string
	Bic                   string
	Note                  string
	Lines                 []InvoiceLine
	PdfInvoiceFilename    string
	PdfInvoiceData        string
	PdfInvoiceDescription string
}

type InvoiceLine struct {
	Quantity      float64
	Price         float64
	TaxPercentage float64
	Name          string
	Description   string
}

type Address struct {
	StreetName  string
	CityName    string
	PostalZone  string
	CountryCode string
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

	inv.xml.SupplierParty = xmlSupplierParty{
		Party: xmlParty{
			EndpointID: xmlEndpointID{
				Value:    inv.SupplierPeppolID[5:],
				SchemeID: inv.SupplierPeppolID[0:4],
			},
			PartyName:        inv.SupplierName,
			RegistrationName: inv.SupplierName,
			PartyTaxScheme: xmlPartyTaxScheme{
				CompanyID: inv.SupplierVat,
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
				CompanyID: inv.CustomerVat,
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

func (inv *Invoice) addLines() {
	sum := 0.0
	sumTax := make(map[float64]float64)         // Map to handle multiple VAT rates
	taxableAmounts := make(map[float64]float64) // Map to track taxable amounts per VAT rate

	// Iterate over invoice lines to calculate totals
	for i, line := range inv.Lines {
		lineAmountExcl := round(line.Quantity * line.Price)
		tax := round(lineAmountExcl * line.TaxPercentage / 100)

		// Update sums for taxable amounts and taxes
		sum += lineAmountExcl
		sumTax[line.TaxPercentage] += tax
		taxableAmounts[line.TaxPercentage] += lineAmountExcl

		invoiceLine := xmlInvoiceLine{
			ID:                  strconv.Itoa(i + 1),
			InvoicedQuantity:    xmlQuantity{Value: line.Quantity, UnitCode: "ZZ"},
			LineExtensionAmount: xmlAmount{Value: lineAmountExcl, CurrencyID: "EUR"},
			TaxTotal: xmlTaxTotal{
				TaxAmount: xmlAmount{Value: tax, CurrencyID: "EUR"},
			},
			Item: xmlItem{
				Name: line.Name,
				Description: func() string {
					if line.Description != "" {
						return line.Description
					}
					return ""
				}(),
				ClassifiedTaxCategory: xmlTaxCategory{
					ID:      "S",
					Name:    "Standard rated",
					Percent: line.TaxPercentage,
					TaxScheme: xmlTaxScheme{
						ID: "VAT",
					},
				},
			},
			Price: xmlPrice{
				PriceAmount: xmlAmount{
					Value:      line.Price,
					CurrencyID: "EUR",
				},
			},
		}
		inv.xml.InvoiceLines = append(inv.xml.InvoiceLines, invoiceLine)
	}

	// Calculate tax subtotals and totals
	taxSubtotals := []xmlTaxSubtotal{}
	totalTax := 0.0
	for rate, taxAmount := range sumTax {
		taxableAmount := taxableAmounts[rate]
		taxSubtotals = append(taxSubtotals, xmlTaxSubtotal{
			TaxableAmount: xmlAmount{
				Value:      taxableAmount,
				CurrencyID: "EUR",
			},
			TaxAmount: xmlAmount{
				Value:      taxAmount,
				CurrencyID: "EUR",
			},
			TaxCategory: xmlTaxCategory{
				ID:      "S",
				Name:    "Standard rated",
				Percent: rate,
				TaxScheme: xmlTaxScheme{
					ID: "VAT",
				},
			},
		})
		totalTax += taxAmount
	}

	total := round(sum + totalTax)

	inv.xml.TaxTotal = xmlTaxTotal{
		TaxAmount:   xmlAmount{Value: totalTax, CurrencyID: "EUR"},
		TaxSubtotal: taxSubtotals,
	}

	inv.xml.LegalMonetaryTotal = xmlMonetaryTotal{
		LineExtensionAmount: xmlAmount{Value: sum, CurrencyID: "EUR"},
		TaxExclusiveAmount:  xmlAmount{Value: sum, CurrencyID: "EUR"},
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
	DueDate                     string                 `xml:"cbc:DueDate"`
	CreditNoteTypeCode          string                 `xml:"cbc:CreditNoteTypeCode"`
	DocumentCurrency            string                 `xml:"cbc:DocumentCurrencyCode"`
	OrderReference              string                 `xml:"cac:OrderReference>cbc:ID"`
	SupplierParty               xmlSupplierParty       `xml:"cac:AccountingSupplierParty"`
	CustomerParty               xmlCustomerParty       `xml:"cac:AccountingCustomerParty"`
	PaymentMeans                xmlPaymentMeans        `xml:"cac:PaymentMeans"`
	PaymentTerms                xmlPaymentTerms        `xml:"cac:PaymentTerms,omitempty"`
	CreditNoteLines             []xmlCreditNoteLine    `xml:"cac:CreditNoteLine"`
	AdditionalDocumentReference []xmlDocumentReference `xml:"cac:AdditionalDocumentReference,omitempty"`
	TaxTotal                    xmlTaxTotal            `xml:"cac:TaxTotal"`
	LegalMonetaryTotal          xmlMonetaryTotal       `xml:"cac:LegalMonetaryTotal"`
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
		IssueDate:          time.Now().Format("2006-01-02"),
		DueDate:            time.Now().AddDate(0, 0, 30).Format("2006-01-02"),
		CreditNoteTypeCode: "381",
		DocumentCurrency:   "EUR",
		ID:                 cn.ID,
		OrderReference:     cn.ID,
	}

	cn.xml.SupplierParty = xmlSupplierParty{
		Party: xmlParty{
			EndpointID: xmlEndpointID{
				Value:    cn.SupplierPeppolID[5:],
				SchemeID: cn.SupplierPeppolID[0:4],
			},
			PartyName:        cn.SupplierName,
			RegistrationName: cn.SupplierName,
			PartyTaxScheme: xmlPartyTaxScheme{
				CompanyID: cn.SupplierVat,
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
				CompanyID: cn.CustomerVat,
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
	sum := 0.0
	sumTax := make(map[float64]float64)         // Map to handle multiple VAT rates
	taxableAmounts := make(map[float64]float64) // Map to track taxable amounts per VAT rate

	// Iterate over invoice lines to calculate totals
	for i, line := range cn.Lines {
		lineAmountExcl := round(line.Quantity * line.Price)
		tax := round(lineAmountExcl * line.TaxPercentage / 100)

		// Update sums for taxable amounts and taxes
		sum += lineAmountExcl
		sumTax[line.TaxPercentage] += tax
		taxableAmounts[line.TaxPercentage] += lineAmountExcl

		invoiceLine := xmlCreditNoteLine{
			ID:                  strconv.Itoa(i + 1),
			CreditedQuantity:    xmlQuantity{Value: line.Quantity, UnitCode: "ZZ"},
			LineExtensionAmount: xmlAmount{Value: lineAmountExcl, CurrencyID: "EUR"},
			Item: xmlItem{
				Name: line.Name,
				Description: func() string {
					if line.Description != "" {
						return line.Description
					}
					return ""
				}(),
				ClassifiedTaxCategory: xmlTaxCategory{
					ID:      "S",
					Name:    "Standard rated",
					Percent: line.TaxPercentage,
					TaxScheme: xmlTaxScheme{
						ID: "VAT",
					},
				},
			},
			Price: xmlPrice{
				PriceAmount: xmlAmount{
					Value:      line.Price,
					CurrencyID: "EUR",
				},
			},
		}
		cn.xml.CreditNoteLines = append(cn.xml.CreditNoteLines, invoiceLine)
	}

	// Calculate tax subtotals and totals
	taxSubtotals := []xmlTaxSubtotal{}
	totalTax := 0.0
	for rate, taxAmount := range sumTax {
		taxableAmount := taxableAmounts[rate]
		taxSubtotals = append(taxSubtotals, xmlTaxSubtotal{
			TaxableAmount: xmlAmount{
				Value:      taxableAmount,
				CurrencyID: "EUR",
			},
			TaxAmount: xmlAmount{
				Value:      taxAmount,
				CurrencyID: "EUR",
			},
			TaxCategory: xmlTaxCategory{
				ID:      "S",
				Name:    "Standard rated",
				Percent: rate,
				TaxScheme: xmlTaxScheme{
					ID: "VAT",
				},
			},
		})
		totalTax += taxAmount
	}

	total := round(sum + totalTax)

	cn.xml.TaxTotal = xmlTaxTotal{
		TaxAmount:   xmlAmount{Value: totalTax, CurrencyID: "EUR"},
		TaxSubtotal: taxSubtotals,
	}

	cn.xml.LegalMonetaryTotal = xmlMonetaryTotal{
		LineExtensionAmount: xmlAmount{Value: sum, CurrencyID: "EUR"},
		TaxExclusiveAmount:  xmlAmount{Value: sum, CurrencyID: "EUR"},
		TaxInclusiveAmount:  xmlAmount{Value: total, CurrencyID: "EUR"},
		PayableAmount:       xmlAmount{Value: total, CurrencyID: "EUR"},
	}
}
