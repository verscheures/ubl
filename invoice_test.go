package ubl_test

import (
	"testing"

	"github.com/verscheures/ubl"
	"github.com/verscheures/ubl/validate"
)

func TestNewInvoice(t *testing.T) {

	inv := ubl.Invoice{
		ID:               "INV-12345",
		SupplierName:     "ABC Supplies Ltd",
		SupplierVat:      "BE0123456789",
		SupplierPeppolID: "9925:BE0123456789",
		SupplierAddress: ubl.Address{
			StreetName:  "123 Supplier Street",
			CityName:    "Supplier City",
			PostalZone:  "12345",
			CountryCode: "BE",
		},
		CustomerName:     "XYZ Corp",
		CustomerVat:      "BE9876543210",
		CustomerPeppolID: "9925:BE9876543210",
		CustomerAddress: ubl.Address{
			StreetName:  "789 Customer Avenue",
			CityName:    "Customer Town",
			PostalZone:  "67890",
			CountryCode: "BE",
		},
		Iban:               "9999999999",
		Bic:                "GEBABEBB",
		Note:               "You get a free sticker when you pay fast",
		PdfInvoiceFilename: "invoice_test.pdf",
	}

	inv.Lines = []ubl.InvoiceLine{
		{
			Quantity:      10,
			Price:         100,
			Name:          "Product A - Standard rated",
			Description:   "Standard 21% VAT item",
			TaxPercentage: 21.0,
			TaxCategoryID: "S",
		},
		{
			Quantity:      5,
			Price:         50,
			Name:          "Product B - Reduced rate",
			Description:   "Reduced 6% VAT item",
			TaxPercentage: 6.0,
			TaxCategoryID: "S",
		},
		{
			Quantity:      2,
			Price:         200,
			Name:          "Product C - Zero rated",
			Description:   "Zero rated item",
			TaxPercentage: 0.0,
			TaxCategoryID: "Z",
		},
		{
			Quantity:      1,
			Price:         500,
			Name:          "Product D - Exempt",
			Description:   "VAT exempt item",
			TaxPercentage: 0.0,
			TaxCategoryID: "E",
		},
	}

	xmlBytes, err := inv.Generate()
	if err != nil {
		t.Error(err)
	}

	v, err := validate.New()
	if err != nil {
		t.Error(err)
	}

	defer v.Free()

	err = v.ValidateBytes(xmlBytes)
	if err != nil {
		t.Error(err)
	}

	// // Write to file or print
	// err = os.WriteFile("invoice.xml", xmlBytes, 0644)
	// if err != nil {
	// 	fmt.Println("Error writing XML file:", err)
	// }

}
