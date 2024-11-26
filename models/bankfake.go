package models

import (
	"encoding/json"
	"fmt"
	"github.com/go-playground/validator/v10"
)

type BankFake struct {
	ID             uint            `gorm:"primaryKey" json:"id"`
	BankName       string          `json:"bankName" gorm:"not null"`
	BankShortName  string          `json:"bankShortName" gorm:"not null"`
	AccountNumbers json.RawMessage `json:"accountNumbers" gorm:"type:json" validate:"required"`
	Icon           string          `json:"icon" gorm:"not null"`
}

func validateAccountNumber(bankShortName string, accountNumbers json.RawMessage) error {
	var accounts []string
	if err := json.Unmarshal(accountNumbers, &accounts); err != nil {
		return fmt.Errorf("định dạng số tài khoản không hợp lệ: %v", err)
	}

	for _, account := range accounts {
		length := len(account)
		switch bankShortName {
		case "SACOMBANK", "VIETINBANK":
			if length != 12 {
				return fmt.Errorf("số tài khoản của %s phải có 12 chữ số", bankShortName)
			}
		case "VCB", "AGRIBANK":
			if length < 13 || length > 16 {
				return fmt.Errorf("số tài khoản của %s phải có 13 chữ số", bankShortName)
			}
		case "MB":
			if length < 9 || length > 13 {
				return fmt.Errorf("số tài khoản của %s phải có từ 9 đến 13 chữ số", bankShortName)
			}
		case "TCB", "BIDV":
			if length != 14 {
				return fmt.Errorf("số tài khoản của %s phải có 14 chữ số", bankShortName)
			}
		case "ACB":
			if length != 8 && length != 9 {
				return fmt.Errorf("số tài khoản của %s phải có 8 hoặc 9 chữ số", bankShortName)
			}
		case "SCB":
			if length != 8 && length != 10 {
				return fmt.Errorf("số tài khoản của %s phải có 8 hoặc 10 chữ số", bankShortName)
			}
		case "VPBANK":
			if length < 8 || length > 9 {
				return fmt.Errorf("số tài khoản của %s phải có từ 8 đến 9 chữ số", bankShortName)
			}
		default:
			return fmt.Errorf("không có quy tắc xác thực cho ngân hàng: %s", bankShortName)
		}
	}
	return nil
}

func (b *BankFake) Validate() error {
	validate := validator.New()

	if err := validate.Struct(b); err != nil {
		return err
	}

	return validateAccountNumber(b.BankShortName, b.AccountNumbers)
}
