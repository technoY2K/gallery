package model

import (
	"regexp"
	"strings"

	"github.com/jhampac/gallery/hasho"
	"github.com/jhampac/gallery/rando"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"golang.org/x/crypto/bcrypt"
)

const (
	ErrNotFound          modelError = "model: resource not found"
	ErrIDInvalid         modelError = "model: ID provided was invalid"
	ErrPasswordInccorect modelError = "model: incorrect password provided"
	ErrEmailRequired     modelError = "model: email address is required"
	ErrEmailInvalid      modelError = "model: email address is not valid"
	ErrEmailTaken        modelError = "model: email address is already taken"
	ErrPasswordTooShort  modelError = "model: password must be at least 8 characters long"
	ErrPasswordRequired  modelError = "model: password is required"
	ErrRememberRequited  modelError = "model: remember token is required"
	ErrRememberTooShort  modelError = "model: remember token must be at least 32 bytes long"
)

const (
	pepper        = "suns-in-7"
	hmacSecretKey = "change-this-secert-later-for-production"
)

type modelError string

func (e modelError) Error() string {
	return string(e)
}

func (e modelError) Public() string {
	s := strings.Replace(string(e), "model: ", "", 1)
	split := strings.Split(s, " ")
	split[0] = strings.Title(split[0])
	return strings.Join(split, " ")
}

type UserService interface {
	Authenticate(email string, password string) (*User, error)
	UserDB
}

type userService struct {
	UserDB
}

var _ UserService = &userService{}

type UserDB interface {
	// db look ups with args
	ByID(id uint) (*User, error)
	ByEmail(email string) (*User, error)
	ByRemember(token string) (*User, error)

	// altering db data
	Create(user *User) error
	Update(user *User) error
	Delete(id uint) error

	// db operations
	Close() error
	AutoMigrate() error
	DestructiveReset() error
}

// user db representation
type User struct {
	gorm.Model
	Name         string
	Email        string `gorm:"not null;unique_index"`
	Password     string `gorm:"-"`
	PasswordHash string `gorm:"not null"`
	Remember     string `gorm:"-"`
	RememberHash string `gorm:"not null;unique_index"`
}

type userGorm struct {
	db *gorm.DB
}

var _ UserDB = &userGorm{}

type userValidator struct {
	UserDB
	hmac       hasho.HMAC
	emailRegex *regexp.Regexp
}

type userValFn func(*User) error

func NewUserService(connInfo string) (UserService, error) {
	ug, err := newUserGorm(connInfo)
	if err != nil {
		return nil, err
	}

	hmac := hasho.NewHMAC(hmacSecretKey)
	uv := newUserValidator(ug, hmac)

	return &userService{
		UserDB: uv,
	}, nil
}

func newUserValidator(udb UserDB, hmac hasho.HMAC) *userValidator {
	return &userValidator{
		UserDB:     udb,
		hmac:       hmac,
		emailRegex: regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,16}$`),
	}
}

func newUserGorm(connInfo string) (*userGorm, error) {
	db, err := gorm.Open("postgres", connInfo)
	if err != nil {
		return nil, err
	}
	db.LogMode(true)

	return &userGorm{
		db: db,
	}, nil
}

func first(db *gorm.DB, dst interface{}) error {
	err := db.First(dst).Error
	if err == gorm.ErrRecordNotFound {
		return ErrNotFound
	}
	return err
}

func (uv *userValidator) Create(user *User) error {
	err := runUserValidations(user,
		uv.passwordRequired,
		uv.passwordMinLength,
		uv.bcryptPassword,
		uv.passwordHashRequired,
		uv.setRememberIfUnset,
		uv.rememberMinBytes,
		uv.hmacRemember,
		uv.rememberHashRequired,
		uv.normalizeEmail,
		uv.requireEmail,
		uv.emailFormat,
		uv.emailIsAvailable)
	if err != nil {
		return err
	}

	return uv.UserDB.Create(user)
}

func (ug *userGorm) Create(user *User) error {
	return ug.db.Create(user).Error
}

func (ug *userGorm) ByID(id uint) (*User, error) {
	var user User
	db := ug.db.Where("id = ?", id)
	err := first(db, &user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (uv *userValidator) ByEmail(email string) (*User, error) {
	user := User{
		Email: email,
	}

	err := runUserValidations(&user, uv.normalizeEmail)
	if err != nil {
		return nil, err
	}

	return uv.UserDB.ByEmail(user.Email)
}

func (ug *userGorm) ByEmail(email string) (*User, error) {
	var user User
	db := ug.db.Where("email = ?", email)
	err := first(db, &user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (uv *userValidator) ByRemember(token string) (*User, error) {
	user := User{
		Remember: token,
	}
	if err := runUserValidations(&user, uv.hmacRemember); err != nil {
		return nil, err
	}
	return uv.UserDB.ByRemember(user.RememberHash)
}

func (ug *userGorm) ByRemember(rememberHash string) (*User, error) {
	var user User
	err := first(ug.db.Where("remember_hash = ?", rememberHash), &user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (uv *userValidator) Update(user *User) error {
	err := runUserValidations(user,
		uv.passwordMinLength,
		uv.bcryptPassword,
		uv.passwordHashRequired,
		uv.rememberMinBytes,
		uv.hmacRemember,
		uv.rememberHashRequired,
		uv.normalizeEmail,
		uv.requireEmail,
		uv.emailFormat,
		uv.emailIsAvailable)
	if err != nil {
		return err
	}

	return uv.UserDB.Update(user)
}

func (ug *userGorm) Update(user *User) error {
	return ug.db.Save(user).Error
}

func (uv *userValidator) Delete(id uint) error {
	user := new(User)
	user.ID = id
	err := runUserValidations(user, uv.idGreaterThan(10))
	if err != nil {
		return err
	}

	return uv.UserDB.Delete(id)
}

func (ug *userGorm) Delete(id uint) error {
	user := User{Model: gorm.Model{ID: id}}
	return ug.db.Delete(&user).Error
}

func (ug *userGorm) Close() error {
	return ug.db.Close()
}

func (ug *userGorm) DestructiveReset() error {
	err := ug.db.DropTableIfExists(&User{}).Error
	if err != nil {
		return err
	}
	return ug.AutoMigrate()
}

func (ug *userGorm) AutoMigrate() error {
	if err := ug.db.AutoMigrate(&User{}).Error; err != nil {
		return err
	}
	return nil
}

func (uv *userValidator) bcryptPassword(user *User) error {
	if user.Password == "" {
		return nil
	}

	pwBytes := []byte(user.Password + pepper)
	hashedBytes, err := bcrypt.GenerateFromPassword(pwBytes, bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hashedBytes)
	user.Password = ""
	return nil
}

func (uv *userValidator) hmacRemember(user *User) error {
	if user.Remember == "" {
		return nil
	}
	user.RememberHash = uv.hmac.Hash(user.Remember)
	return nil
}

func (uv *userValidator) setRememberIfUnset(user *User) error {
	if user.Remember != "" {
		return nil
	}

	token, err := rando.RememberToken()
	if err != nil {
		return err
	}

	user.Remember = token
	return nil
}

func (uv *userValidator) idGreaterThan(n uint) userValFn {
	return userValFn(func(user *User) error {
		if user.ID <= n {
			return ErrIDInvalid
		}
		return nil
	})
}

func (uv *userValidator) normalizeEmail(user *User) error {
	user.Email = strings.ToLower(user.Email)
	user.Email = strings.TrimSpace(user.Email)
	return nil
}

func (uv *userValidator) requireEmail(user *User) error {
	if user.Email == "" {
		return ErrEmailRequired
	}
	return nil
}

func (uv *userValidator) emailFormat(user *User) error {
	if user.Email == "" {
		return nil
	}

	if !uv.emailRegex.MatchString(user.Email) {
		return ErrEmailInvalid
	}

	return nil
}

func (uv *userValidator) emailIsAvailable(user *User) error {
	existing, err := uv.ByEmail(user.Email)
	if err == ErrNotFound {
		return nil
	}

	// some error happened other than not found
	if err != nil {
		return err
	}

	if user.ID != existing.ID {
		return ErrEmailTaken
	}

	return nil
}

func (uv *userValidator) passwordMinLength(user *User) error {
	if user.Password == "" {
		return nil
	}

	if len(user.Password) < 8 {
		return ErrPasswordTooShort
	}

	return nil
}

func (uv *userValidator) passwordRequired(user *User) error {
	if user.Password == "" {
		return ErrPasswordRequired
	}
	return nil
}

func (uv *userValidator) passwordHashRequired(user *User) error {
	if user.PasswordHash == "" {
		return ErrPasswordRequired
	}
	return nil
}

func (uv *userValidator) rememberMinBytes(user *User) error {
	if user.Remember == "" {
		return nil
	}

	n, err := rando.NBytes(user.Remember)
	if err != nil {
		return err
	}

	if n < 32 {
		return ErrRememberTooShort
	}

	return nil
}

func (uv *userValidator) rememberHashRequired(user *User) error {
	if user.RememberHash == "" {
		return ErrRememberRequited
	}
	return nil
}

func runUserValidations(user *User, fns ...userValFn) error {
	for _, fn := range fns {
		if err := fn(user); err != nil {
			return err
		}
	}
	return nil
}

func (us *userService) Authenticate(email string, password string) (*User, error) {
	foundUser, err := us.ByEmail(email)
	if err != nil {
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(foundUser.PasswordHash), []byte(password+pepper))
	switch err {
	case nil:
		return foundUser, nil
	case bcrypt.ErrMismatchedHashAndPassword:
		return nil, ErrPasswordInccorect
	default:
		return nil, err
	}
}
